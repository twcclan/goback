---------------------------------- MODULE GC ----------------------------------
(* The store's garbage collection protocol: writers deduplicate against what
   the store says it holds, one maintainer condemns unreachable objects with
   tombstones, waits out the sessions that may have deduplicated before the
   tombstones, confirms with a second mark and only then deletes. With
   Resurrect, committing sessions take back what they relied on instead of
   being waited out; with Pipelined, each mark confirms the previous
   generation and condemns for the next, as the store runs it.

   Safe: no committed backup ever references an object without a committed
   copy. *)
EXTENDS Naturals, FiniteSets

CONSTANTS
    Objects,     \* object hashes
    Edges,       \* 10 * parent + child: a tree or file references its children
    Sessions,    \* each session runs once
    Maintainers, \* processes that may run maintenance
    Versions,    \* the write versions a record may get
    Monotone,    \* TRUE: every write gets a newer version than all before it
    MaxCycles,   \* leases the model hands out
    SkipWait,    \* mutation: delete without waiting for older sessions
    SkipConfirm, \* mutation: delete on the first mark alone
    SkipTombs,   \* mutation: Has ignores tombstones
    SkipSnapshot, \* mutation: a rewrite also drops copies written after the snapshot
    NoFence,     \* mutation: maintainers ignore the lease
    Resurrect,   \* sessions resurrect at commit instead of being waited out
    SkipSealCheck, \* mutation: a committing session ignores the seal
    EarlyUntombs, \* mutation: the maintainer reads un-tombstones before sealing
    SkipClosure, \* mutation: an un-tombstone keeps its object but not what it references
    Rewriters,   \* processes that only execute plans other maintainers published
    Handoff,     \* a maintainer publishes its plan for a rewriter instead of deleting
    SkipPlanWait, \* mutation: a mark starts while a published plan is still pending
    Pipelined,   \* one mark confirms the previous generation and condemns for the next
    SealScope,   \* a committing session only re-checks seals written after it began
    SkipUntombRoots, \* mutation: a pipelined mark does not keep what un-tombstones take back
    TombsNewer   \* a pipelined condemnation checks its tombstones are newer than the snapshot's copies

None == "none"

VARIABLES
    records,  \* the bucket: copies and tombstones
    clock,    \* the newest version written, for Monotone
    phase,    \* per session: new, active, committed, aborted
    refs,     \* per session: the objects its backup will reference
    roots,    \* <<session, object>> for every object a committed backup references
    gc,       \* per maintainer
    lease,    \* the maintainer holding the lease, or None
    cycles,
    dedup,    \* per session: the objects it deduplicated instead of uploading
    sealed,   \* the tombstones a maintainer sealed before reading un-tombstones
    plans,    \* the plans published to the bucket and not yet removed
    rw,       \* per rewriter
    state,    \* the previous generation's record in the bucket, for a pipelined mark
    seen      \* per session: the tombstones sealed before it began

vars == <<records, clock, phase, refs, roots, gc, lease, cycles, dedup, sealed, plans, rw, state, seen>>

\* writer tells copies apart: each session writes an object at most once, and
\* the maintainer writes tombstones
Record(kind, o, v, writer, owner) == [kind |-> kind, obj |-> o, ver |-> v, writer |-> writer, owner |-> owner]

Children(o) == {c \in Objects : 10 * o + c \in Edges}

\* everything a set of objects references, themselves included
RECURSIVE Closure(_)
Closure(S) == IF S = {} THEN {} ELSE S \cup Closure(UNION {Children(o) : o \in S})

Reachable == Closure({p[2] : p \in roots})

Committed == {r \in records : r.owner = None}

\* the records a session's lookups see: everything committed plus its own
\* pending uploads
Visible(s) == {r \in records : r.owner \in {None, s}}

\* an un-tombstone takes a tombstone back when it is newer, or when its
\* session was still live as the tombstone was sealed
Revoked(t, untombs, live) == \E u \in untombs : u.obj = t.obj /\ (u.ver > t.ver \/ u.writer \in live)

\* Has: the newest record of the object decides; a tie goes to the tombstone
Present(s, o) ==
    LET untombs == {u \in Visible(s) : u.kind = "untomb"}
    IN \E c \in Visible(s) :
        /\ c.kind = "copy" /\ c.obj = o
        /\ SkipTombs \/ \A t \in Visible(s) :
              (t.kind = "tomb" /\ t.obj = o) => (t.ver < c.ver \/ Revoked(t, untombs, {}))

Idle == [pc |-> "idle", m1 |-> {}, m2 |-> {}, snap |-> {}, tombs |-> {}, untombs |-> {}, older |-> {}, live |-> {}, drop |-> {}]

NoState == [valid |-> FALSE, snap |-> {}, m1 |-> {}, tombs |-> {}, untombs |-> {}, live |-> {}]

Ended(s) == phase[s] \in {"committed", "aborted"}

NewVersions == IF Monotone THEN {clock + 1} ELSE Versions

Init ==
    /\ records = {}
    /\ clock = 0
    /\ phase = [s \in Sessions |-> "new"]
    /\ refs = [s \in Sessions |-> {}]
    /\ roots = {}
    /\ gc = [m \in Maintainers |-> Idle]
    /\ lease = None
    /\ cycles = 0
    /\ dedup = [s \in Sessions |-> {}]
    /\ sealed = {}
    /\ plans = {}
    /\ rw = [w \in Rewriters |-> [pc |-> "idle", plan |-> {}]]
    /\ state = NoState
    /\ seen = [s \in Sessions |-> {}]

(* sessions *)

Begin(s) ==
    /\ phase[s] = "new"
    /\ phase' = [phase EXCEPT ![s] = "active"]
    /\ seen' = [seen EXCEPT ![s] = sealed]
    /\ UNCHANGED <<records, clock, refs, roots, gc, lease, cycles, dedup, sealed, plans, rw, state>>

\* the session needs o: it deduplicates when the store says o is present and
\* uploads its own copy otherwise
Use(s, o) ==
    /\ phase[s] = "active"
    /\ o \notin refs[s]
    /\ refs' = [refs EXCEPT ![s] = @ \cup {o}]
    /\ IF Present(s, o)
         THEN /\ dedup' = [dedup EXCEPT ![s] = @ \cup {o}]
              /\ UNCHANGED <<records, clock, state, seen>>
         ELSE /\ \E v \in NewVersions :
                   /\ records' = records \cup {Record("copy", o, v, s, s)}
                   /\ clock' = IF v > clock THEN v ELSE clock
              /\ UNCHANGED dedup
    /\ UNCHANGED <<phase, roots, gc, lease, cycles, sealed, plans, rw, state, seen>>

\* before committing, take back every tombstone of what the session
\* deduplicated
Untomb(s) ==
    /\ Resurrect
    /\ phase[s] = "active"
    /\ phase' = [phase EXCEPT ![s] = "untombed"]
    /\ \E ver \in [dedup[s] -> NewVersions] :
         /\ records' = records \cup {Record("untomb", o, ver[o], s, None) : o \in dedup[s]}
         /\ clock' = IF dedup[s] = {} THEN clock ELSE
                       LET top == CHOOSE v \in {ver[o] : o \in dedup[s]} : \A o \in dedup[s] : ver[o] <= v
                       IN IF top > clock THEN top ELSE clock
    /\ UNCHANGED <<refs, roots, gc, lease, cycles, dedup, sealed, plans, rw, state, seen>>

\* after its un-tombstones are written, a session uploads again whatever a
\* sealed tombstone condemns among what it deduplicated and all that
\* references
Check(s) ==
    LET checked == IF SealScope THEN sealed \ seen[s] ELSE sealed
        redo == IF SkipSealCheck THEN {} ELSE {o \in Closure(dedup[s]) : \E t \in checked : t.obj = o}
    IN /\ Resurrect
       /\ phase[s] = "untombed"
       \* a re-upload copies the old copy; without one the session can only abort
       /\ \A o \in redo : \E c \in Committed : c.kind = "copy" /\ c.obj = o
       /\ phase' = [phase EXCEPT ![s] = "checked"]
       /\ \E ver \in [redo -> NewVersions] :
            /\ records' = records \cup {Record("copy", o, ver[o], s, s) : o \in redo}
            /\ clock' = IF redo = {} THEN clock ELSE
                          LET top == CHOOSE v \in {ver[o] : o \in redo} : \A o \in redo : ver[o] <= v
                          IN IF top > clock THEN top ELSE clock
       /\ UNCHANGED <<refs, roots, gc, lease, cycles, dedup, sealed, plans, rw, state, seen>>

\* an agent uploads an object only after handling what it references
Commit(s) ==
    /\ phase[s] = IF Resurrect THEN "checked" ELSE "active"
    /\ \A r \in records : (r.kind = "copy" /\ r.owner = s /\ r.obj \notin dedup[s]) => Children(r.obj) \subseteq refs[s]
    /\ phase' = [phase EXCEPT ![s] = "committed"]
    /\ records' = {IF r.owner = s THEN [r EXCEPT !.owner = None] ELSE r : r \in records}
    /\ roots' = roots \cup {<<s, o>> : o \in refs[s]}
    /\ UNCHANGED <<clock, refs, gc, lease, cycles, dedup, sealed, plans, rw, state, seen>>

\* a session ends without committing, by choice or because the reaper ended
\* it after a crash; its end marker keeps it from ever committing
Abort(s) ==
    /\ phase[s] \in {"active", "untombed", "checked"}
    /\ phase' = [phase EXCEPT ![s] = "aborted"]
    /\ records' = {r \in records : r.owner # s}
    /\ UNCHANGED <<clock, refs, roots, gc, lease, cycles, dedup, sealed, plans, rw, state, seen>>

\* a backup is deleted, so what only it referenced becomes unreachable
Retire(p) ==
    /\ roots' = roots \ {p}
    /\ UNCHANGED <<records, clock, phase, refs, gc, lease, cycles, dedup, sealed, plans, rw, state, seen>>

(* the maintainers: one holds the lease at a time, but a maintainer whose
   lease ran out may not know it yet *)

\* every step re-checks the lease, except the deletes that follow the last
\* check
Holds(m) == NoFence \/ lease = m

Acquire(m) ==
    /\ lease = None
    /\ gc[m].pc = "idle"
    /\ cycles < MaxCycles
    /\ lease' = m
    /\ cycles' = cycles + 1
    /\ UNCHANGED <<records, clock, phase, refs, roots, gc, dedup, sealed, plans, rw, state, seen>>

\* the holder stalls past its lease
Expire ==
    /\ lease # None
    /\ lease' = None
    /\ UNCHANGED <<records, clock, phase, refs, roots, gc, cycles, dedup, sealed, plans, rw, state, seen>>

Mark(m) ==
    /\ gc[m].pc = "idle"
    /\ Holds(m)
    /\ SkipPlanWait \/ plans = {}
    /\ gc' = [gc EXCEPT ![m].pc = "marked", ![m].m1 = Reachable,
                        ![m].snap = {c \in Committed : c.kind = "copy"}]
    /\ UNCHANGED <<records, clock, phase, refs, roots, lease, cycles, dedup, sealed, plans, rw, state, seen>>

\* one tombstone archive for every unmarked object with a copy in the snapshot
Condemn(m) ==
    LET doomed == {o \in Objects \ gc[m].m1 : \E r \in gc[m].snap : r.obj = o}
    IN /\ gc[m].pc = "marked"
       /\ Holds(m)
       /\ \E ver \in [doomed -> NewVersions] :
            /\ records' = records \cup {Record("tomb", o, ver[o], m, None) : o \in doomed}
            /\ clock' = IF doomed = {} THEN clock ELSE
                          LET top == CHOOSE v \in {ver[o] : o \in doomed} : \A o \in doomed : ver[o] <= v
                          IN IF top > clock THEN top ELSE clock
       /\ gc' = [gc EXCEPT ![m].pc = "condemned"]
       /\ UNCHANGED <<phase, refs, roots, lease, cycles, dedup, sealed, plans, rw, state, seen>>

\* after the tombstones are written, list the sessions that might have
\* deduplicated before them; every tombstone in the store is handled
Horizon(m) ==
    /\ gc[m].pc = "condemned"
    /\ Holds(m)
    /\ gc' = [gc EXCEPT ![m].pc = "listed",
                        ![m].older = {s \in Sessions : ~Ended(s)},
                        ![m].tombs = {r \in Committed : r.kind = "tomb"},
                        ![m].untombs = {r \in Committed : r.kind = "untomb"}]
    /\ UNCHANGED <<records, clock, phase, refs, roots, lease, cycles, dedup, sealed, plans, rw, state, seen>>

\* sessions check the seal after writing their un-tombstones, and the
\* maintainer reads un-tombstones after sealing: one side sees the other
Seal(m) ==
    /\ Resurrect
    /\ gc[m].pc = "listed"
    /\ Holds(m)
    /\ sealed' = sealed \cup gc[m].tombs
    /\ gc' = [gc EXCEPT ![m].pc = "sealed", ![m].live = {s \in Sessions : ~Ended(s)}]
    /\ UNCHANGED <<records, clock, phase, refs, roots, lease, cycles, dedup, plans, rw, state, seen>>

Confirm(m) ==
    /\ gc[m].pc = IF Resurrect THEN "sealed" ELSE "listed"
    /\ Holds(m)
    /\ SkipWait \/ Resurrect \/ \A s \in gc[m].older : Ended(s)
    /\ gc' = [gc EXCEPT ![m].pc = "confirmed", ![m].m2 = IF SkipConfirm THEN gc[m].m1 ELSE Reachable]
    /\ UNCHANGED <<records, clock, phase, refs, roots, lease, cycles, dedup, sealed, plans, rw, state, seen>>

\* pick the snapshot copies older than a tombstone of an object still
\* unmarked, and the tombstones whose object is marked again or has no older
\* copy left
Plan(m) ==
    LET g == gc[m]
        tombs == g.tombs \cap records
        untombs == IF EarlyUntombs THEN g.untombs \cap records ELSE {r \in records : r.kind = "untomb"}
        \* what a live session's un-tombstones take back is a root
        kept == IF SkipClosure THEN {} ELSE Closure({u.obj : u \in {v \in untombs : v.writer \in g.live}})
        dead == {c \in records :
                    /\ SkipSnapshot \/ c \in g.snap
                    /\ c.kind = "copy" /\ c.owner = None /\ c.obj \notin g.m2 \cup kept
                    /\ \E t \in tombs : t.obj = c.obj /\ c.ver < t.ver /\ ~Revoked(t, untombs, g.live)}
        spent == {t \in tombs :
                    \/ t.obj \in g.m2
                    \/ Revoked(t, untombs, g.live)
                    \/ ~\E c \in records \ dead : c.kind = "copy" /\ c.obj = t.obj /\ c.ver < t.ver}
        \* an un-tombstone goes once its session ended and no tombstone of
        \* its object is left
        idle == {u \in untombs :
                    /\ Ended(u.writer)
                    /\ ~\E t \in {r \in records : r.kind = "tomb"} \ spent : t.obj = u.obj}
    IN /\ g.pc = "confirmed"
       /\ Holds(m)
       /\ gc' = [gc EXCEPT ![m].pc = "planned", ![m].drop = dead \cup spent \cup idle]
       /\ UNCHANGED <<records, clock, phase, refs, roots, lease, cycles, dedup, sealed, plans, rw, state, seen>>

Delete(m) ==
    /\ ~Handoff
    /\ gc[m].pc = "planned"
    /\ records' = records \ gc[m].drop
    /\ gc' = [gc EXCEPT ![m] = Idle]
    /\ lease' = IF lease = m THEN None ELSE lease
    /\ UNCHANGED <<clock, phase, refs, roots, cycles, dedup, sealed, plans, rw, state, seen>>

\* the plan goes to the bucket, and the lease is let go for a rewriter
Publish(m) ==
    /\ Handoff
    /\ gc[m].pc = "planned"
    /\ Holds(m)
    /\ plans' = plans \cup {gc[m].drop}
    /\ gc' = [gc EXCEPT ![m] = Idle]
    /\ lease' = IF lease = m THEN None ELSE lease
    /\ UNCHANGED <<records, clock, phase, refs, roots, cycles, dedup, sealed, rw, state, seen>>

TakePlan(w, plan) ==
    /\ rw[w].pc = "idle"
    /\ lease = None
    /\ cycles < MaxCycles
    /\ plan \in plans
    /\ lease' = w
    /\ cycles' = cycles + 1
    /\ rw' = [rw EXCEPT ![w] = [pc |-> "loaded", plan |-> plan]]
    /\ UNCHANGED <<records, clock, phase, refs, roots, gc, dedup, sealed, plans, state, seen>>

\* the deletes follow the last lease check, as a maintainer's do
Rewrite(w) ==
    /\ rw[w].pc = "loaded"
    /\ records' = records \ rw[w].plan
    /\ rw' = [rw EXCEPT ![w].pc = "rewritten"]
    /\ UNCHANGED <<clock, phase, refs, roots, gc, lease, cycles, dedup, sealed, plans, state, seen>>

\* a rewriter that dies before this leaves the plan to be run again
Finish(w) ==
    /\ rw[w].pc = "rewritten"
    /\ plans' = plans \ {rw[w].plan}
    /\ rw' = [rw EXCEPT ![w] = [pc |-> "idle", plan |-> {}]]
    /\ lease' = IF lease = w THEN None ELSE lease
    /\ UNCHANGED <<records, clock, phase, refs, roots, gc, cycles, dedup, sealed, state, seen>>

RewriterCrash(w) ==
    /\ rw[w].pc # "idle"
    /\ rw' = [rw EXCEPT ![w] = [pc |-> "idle", plan |-> {}]]
    /\ lease' = IF lease = w THEN None ELSE lease
    /\ UNCHANGED <<records, clock, phase, refs, roots, gc, cycles, dedup, sealed, plans, state, seen>>

(* pipelined generations: a mark confirms what the previous generation
   condemned and condemns for the next; un-tombstones carry no writer, so
   every one keeps what it takes back *)

Untombs == {r \in Committed : r.kind = "untomb"}

PMark(m) ==
    LET kept == IF SkipUntombRoots THEN {} ELSE Closure({u.obj : u \in Untombs})
    IN /\ Pipelined
       /\ gc[m].pc = "idle"
       /\ Holds(m)
       /\ SkipPlanWait \/ plans = {}
       /\ gc' = [gc EXCEPT ![m].pc = "marked", ![m].m1 = Reachable \cup kept,
                           ![m].snap = {c \in Committed : c.kind = "copy"},
                           ![m].untombs = Untombs]
       /\ UNCHANGED <<records, clock, phase, refs, roots, lease, cycles, dedup, sealed, plans, rw, state, seen>>

\* drop the previous snapshot's copies both marks left unmarked that a
\* tombstone of the previous horizon condemns and no un-tombstone takes
\* back; tombstones go once taken back or with no older copy left;
\* un-tombstones go once the previous horizon's sessions ended and no
\* tombstone of theirs is left
PPlan(m) ==
    LET g == gc[m]
        tombs == state.tombs \cap records
        dead == {c \in records :
                    /\ c \in state.snap
                    /\ c.kind = "copy" /\ c.owner = None
                    /\ c.obj \notin g.m1 \cup state.m1
                    /\ \E t \in tombs : t.obj = c.obj /\ c.ver < t.ver
                    /\ ~\E u \in g.untombs : u.obj = c.obj}
        \* a copy as old as the tombstone is hidden by it, as Has decides a tie
        spent == {t \in tombs :
                    \/ \E u \in g.untombs : u.obj = t.obj
                    \/ ~\E c \in records : c.kind = "copy" /\ c.obj = t.obj /\ c.ver <= t.ver}
        idle == {u \in state.untombs \cap records :
                    /\ \A s \in state.live : Ended(s)
                    /\ ~\E t \in records : t.kind = "tomb" /\ t.obj = u.obj}
    IN /\ g.pc = "marked"
       /\ Holds(m)
       /\ gc' = [gc EXCEPT ![m].pc = "planned", ![m].drop = IF state.valid THEN dead \cup spent \cup idle ELSE {}]
       /\ UNCHANGED <<records, clock, phase, refs, roots, lease, cycles, dedup, sealed, plans, rw, state, seen>>

PCondemn(m) ==
    LET doomed == {o \in Objects \ gc[m].m1 : \E r \in gc[m].snap : r.obj = o}
    IN /\ gc[m].pc = "planned"
       /\ Holds(m)
       /\ \E ver \in [doomed -> NewVersions] :
            /\ TombsNewer => \A o \in doomed : \A c \in gc[m].snap : c.obj = o => c.ver < ver[o]
            /\ records' = records \cup {Record("tomb", o, ver[o], m, None) : o \in doomed}
            /\ clock' = IF doomed = {} THEN clock ELSE
                          LET top == CHOOSE v \in {ver[o] : o \in doomed} : \A o \in doomed : ver[o] <= v
                          IN IF top > clock THEN top ELSE clock
       /\ gc' = [gc EXCEPT ![m].pc = "condemned"]
       /\ UNCHANGED <<phase, refs, roots, lease, cycles, dedup, sealed, plans, rw, state, seen>>

PHorizon(m) ==
    /\ gc[m].pc = "condemned"
    /\ Holds(m)
    /\ gc' = [gc EXCEPT ![m].pc = "listed",
                        ![m].tombs = {r \in Committed : r.kind = "tomb"},
                        ![m].older = Untombs]
    /\ UNCHANGED <<records, clock, phase, refs, roots, lease, cycles, dedup, sealed, plans, rw, state, seen>>

\* the seal and the generation's record are written after the sessions are
\* listed
PSeal(m) ==
    /\ gc[m].pc = "listed"
    /\ Holds(m)
    /\ sealed' = sealed \cup gc[m].tombs
    /\ state' = [valid |-> TRUE, snap |-> gc[m].snap, m1 |-> gc[m].m1, tombs |-> gc[m].tombs,
                 untombs |-> gc[m].older, live |-> {s \in Sessions : ~Ended(s)}]
    /\ gc' = [gc EXCEPT ![m].pc = "sealed"]
    /\ UNCHANGED <<records, clock, phase, refs, roots, lease, cycles, dedup, plans, rw, seen>>

PDelete(m) ==
    /\ ~Handoff
    /\ gc[m].pc = "sealed"
    /\ records' = records \ gc[m].drop
    /\ gc' = [gc EXCEPT ![m] = Idle]
    /\ lease' = IF lease = m THEN None ELSE lease
    /\ UNCHANGED <<clock, phase, refs, roots, cycles, dedup, sealed, plans, rw, state, seen>>

\* the plan goes to the bucket for a rewriter, as Publish does
PPublish(m) ==
    /\ Handoff
    /\ gc[m].pc = "sealed"
    /\ Holds(m)
    /\ plans' = plans \cup {gc[m].drop}
    /\ gc' = [gc EXCEPT ![m] = Idle]
    /\ lease' = IF lease = m THEN None ELSE lease
    /\ UNCHANGED <<records, clock, phase, refs, roots, cycles, dedup, sealed, rw, state, seen>>

\* a maintainer dies anywhere; what it wrote stays, its lease runs out
Crash(m) ==
    /\ gc[m].pc # "idle"
    /\ gc' = [gc EXCEPT ![m] = Idle]
    /\ lease' = IF lease = m THEN None ELSE lease
    /\ UNCHANGED <<records, clock, phase, refs, roots, cycles, dedup, sealed, plans, rw, state, seen>>

Next ==
    \/ \E s \in Sessions : Begin(s) \/ Untomb(s) \/ Check(s) \/ Commit(s) \/ Abort(s)
    \/ \E s \in Sessions, o \in Objects : Use(s, o)
    \/ \E p \in roots : Retire(p)
    \/ Expire
    \/ \E m \in Maintainers :
         \/ Acquire(m) \/ Crash(m)
         \/ ~Pipelined /\ (Mark(m) \/ Condemn(m) \/ Horizon(m) \/ Seal(m) \/ Confirm(m) \/ Plan(m) \/ Delete(m) \/ Publish(m))
         \/ PMark(m) \/ PPlan(m) \/ PCondemn(m) \/ PHorizon(m) \/ PSeal(m) \/ PDelete(m) \/ PPublish(m)
    \/ \E w \in Rewriters : Rewrite(w) \/ Finish(w) \/ RewriterCrash(w) \/ \E plan \in plans : TakePlan(w, plan)

Spec == Init /\ [][Next]_vars

(* properties *)

Safe == \A o \in Reachable : \E r \in Committed : r.kind = "copy" /\ r.obj = o

TypeOK ==
    /\ phase \in [Sessions -> {"new", "active", "untombed", "checked", "committed", "aborted"}]
    /\ refs \in [Sessions -> SUBSET Objects]
    /\ roots \subseteq Sessions \X Objects
=============================================================================
