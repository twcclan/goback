## backups
* self-contained backups (metadata should be part of the backup)
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
