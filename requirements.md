## backups
* self-contained backups
* namespaces (e.g. users) that share data
* allow restoring a consistent (best effort) snapshot of any given point in time

## indexing
* should allow recreating index from backups
* list all versions of a given file
* list all snapshots

## chunks
* snapshot ( -> []file info)
* file info ( -> file data)
* file data ( -> []chunk data )
* chunk data