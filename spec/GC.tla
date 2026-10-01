---------------------------------- MODULE GC ----------------------------------
(* The store's garbage collection protocol: writers deduplicate against what
   the store says it holds, one maintainer condemns unreachable objects with
   tombstones, waits out the sessions that may have deduplicated before the
   tombstones, confirms with a second mark and only then deletes.

   Safe: no committed backup ever references an object without a committed
   copy. *)
EXTENDS Naturals, FiniteSets

CONSTANTS
    Objects,     \* object hashes
    Sessions,    \* each session runs once
    Versions,    \* the write versions a record may get
    Monotone,    \* TRUE: every write gets a newer version than all before it
    MaxCycles,   \* collections the model runs
    SkipWait,    \* mutation: delete without waiting for older sessions
    SkipConfirm, \* mutation: delete on the first mark alone
    SkipTombs,   \* mutation: Has ignores tombstones
    SkipSnapshot \* mutation: a rewrite also drops copies written after the snapshot

None == "none"

VARIABLES
    records,  \* the bucket: copies and tombstones
    clock,    \* the newest version written, for Monotone
    phase,    \* per session: new, active, committed, aborted
    refs,     \* per session: the objects its backup will reference
    roots,    \* <<session, object>> for every object a committed backup references
    gc        \* the maintainer

vars == <<records, clock, phase, refs, roots, gc>>

\* writer tells copies apart: each session writes an object at most once, and
\* the maintainer writes tombstones
Record(kind, o, v, writer, owner) == [kind |-> kind, obj |-> o, ver |-> v, writer |-> writer, owner |-> owner]

Reachable == {p[2] : p \in roots}

Committed == {r \in records : r.owner = None}

\* the records a session's lookups see: everything committed plus its own
\* pending uploads
Visible(s) == {r \in records : r.owner \in {None, s}}

\* Has: the newest record of the object decides; a tie goes to the tombstone
Present(s, o) ==
    \E c \in Visible(s) :
        /\ c.kind = "copy" /\ c.obj = o
        /\ SkipTombs \/ \A t \in Visible(s) : (t.kind = "tomb" /\ t.obj = o) => t.ver < c.ver

NewVersions == IF Monotone THEN {clock + 1} ELSE Versions

Init ==
    /\ records = {}
    /\ clock = 0
    /\ phase = [s \in Sessions |-> "new"]
    /\ refs = [s \in Sessions |-> {}]
    /\ roots = {}
    /\ gc = [pc |-> "idle", m1 |-> {}, m2 |-> {}, snap |-> {}, tombs |-> {}, older |-> {}, cycles |-> 0]

(* sessions *)

Begin(s) ==
    /\ phase[s] = "new"
    /\ phase' = [phase EXCEPT ![s] = "active"]
    /\ UNCHANGED <<records, clock, refs, roots, gc>>

\* the session needs o: it deduplicates when the store says o is present and
\* uploads its own copy otherwise
Use(s, o) ==
    /\ phase[s] = "active"
    /\ o \notin refs[s]
    /\ refs' = [refs EXCEPT ![s] = @ \cup {o}]
    /\ IF Present(s, o)
         THEN UNCHANGED <<records, clock>>
         ELSE \E v \in NewVersions :
                /\ records' = records \cup {Record("copy", o, v, s, s)}
                /\ clock' = IF v > clock THEN v ELSE clock
    /\ UNCHANGED <<phase, roots, gc>>

Commit(s) ==
    /\ phase[s] = "active"
    /\ phase' = [phase EXCEPT ![s] = "committed"]
    /\ records' = {IF r.owner = s THEN [r EXCEPT !.owner = None] ELSE r : r \in records}
    /\ roots' = roots \cup {<<s, o>> : o \in refs[s]}
    /\ UNCHANGED <<clock, refs, gc>>

\* a session ends without committing, by choice or because the reaper ended
\* it after a crash; its end marker keeps it from ever committing
Abort(s) ==
    /\ phase[s] = "active"
    /\ phase' = [phase EXCEPT ![s] = "aborted"]
    /\ records' = {r \in records : r.owner # s}
    /\ UNCHANGED <<clock, refs, roots, gc>>

\* a backup is deleted, so what only it referenced becomes unreachable
Retire(p) ==
    /\ roots' = roots \ {p}
    /\ UNCHANGED <<records, clock, phase, refs, gc>>

(* the maintainer *)

Mark ==
    /\ gc.pc = "idle"
    /\ gc.cycles < MaxCycles
    /\ gc' = [gc EXCEPT !.pc = "marked", !.m1 = Reachable,
                        !.snap = {c \in Committed : c.kind = "copy"}]
    /\ UNCHANGED <<records, clock, phase, refs, roots>>

\* one tombstone archive for every unmarked object with a copy in the snapshot
Condemn ==
    LET doomed == {o \in Objects \ gc.m1 : \E r \in gc.snap : r.obj = o}
    IN /\ gc.pc = "marked"
       /\ \E ver \in [doomed -> NewVersions] :
            /\ records' = records \cup {Record("tomb", o, ver[o], "gc", None) : o \in doomed}
            /\ clock' = IF doomed = {} THEN clock ELSE
                          LET top == CHOOSE m \in {ver[o] : o \in doomed} : \A o \in doomed : ver[o] <= m
                          IN IF top > clock THEN top ELSE clock
       /\ gc' = [gc EXCEPT !.pc = "condemned"]
       /\ UNCHANGED <<phase, refs, roots>>

\* after the tombstones are written, list the sessions that might have
\* deduplicated before them; every tombstone in the store is handled
Horizon ==
    /\ gc.pc = "condemned"
    /\ gc' = [gc EXCEPT !.pc = "listed",
                        !.older = {s \in Sessions : phase[s] = "active"},
                        !.tombs = {r \in Committed : r.kind = "tomb"}]
    /\ UNCHANGED <<records, clock, phase, refs, roots>>

Confirm ==
    /\ gc.pc = "listed"
    /\ SkipWait \/ \A s \in gc.older : phase[s] \in {"committed", "aborted"}
    /\ gc' = [gc EXCEPT !.pc = "confirmed", !.m2 = IF SkipConfirm THEN gc.m1 ELSE Reachable]
    /\ UNCHANGED <<records, clock, phase, refs, roots>>

\* drop the snapshot copies older than a tombstone of an object still
\* unmarked; drop a tombstone whose object is marked again or has no older
\* copy left
Rewrite ==
    LET tombs == gc.tombs \cap records
        dead == {c \in records :
                    /\ SkipSnapshot \/ c \in gc.snap
                    /\ c.kind = "copy" /\ c.owner = None /\ c.obj \notin gc.m2
                    /\ \E t \in tombs : t.obj = c.obj /\ c.ver < t.ver}
        kept == records \ dead
        spent == {t \in tombs :
                    \/ t.obj \in gc.m2
                    \/ ~\E c \in kept : c.kind = "copy" /\ c.obj = t.obj /\ c.ver < t.ver}
    IN /\ gc.pc = "confirmed"
       /\ records' = kept \ spent
       /\ gc' = [gc EXCEPT !.pc = "idle", !.cycles = @ + 1]
       /\ UNCHANGED <<clock, phase, refs, roots>>

\* the maintainer dies anywhere; what it wrote stays
Crash ==
    /\ gc.pc # "idle"
    /\ gc' = [gc EXCEPT !.pc = "idle", !.cycles = @ + 1]
    /\ UNCHANGED <<records, clock, phase, refs, roots>>

Next ==
    \/ \E s \in Sessions : Begin(s) \/ Commit(s) \/ Abort(s)
    \/ \E s \in Sessions, o \in Objects : Use(s, o)
    \/ \E p \in roots : Retire(p)
    \/ Mark \/ Condemn \/ Horizon \/ Confirm \/ Rewrite \/ Crash

Spec == Init /\ [][Next]_vars

(* properties *)

Safe == \A p \in roots : \E r \in Committed : r.kind = "copy" /\ r.obj = p[2]

TypeOK ==
    /\ phase \in [Sessions -> {"new", "active", "committed", "aborted"}]
    /\ refs \in [Sessions -> SUBSET Objects]
    /\ roots \subseteq Sessions \X Objects
=============================================================================
