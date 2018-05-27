When I first wrote the code it kind of evolved into a program that could handle local single-user backups okay-ish. However, my actual plan was to be able to have
multiple tenants send backup data to a central repository, so as to take advantage of deduplication. Also for convenience I chose to have the objects that represented
a single backup contain the full metadata tree, which would add up to quite a lot of duplicate data for large filesystem trees.

# Current design
It now builds a tree similar to what git does. There are really only two differences:
* I am using protocol buffers to store the data (instead of plain text)
* I added a new object type _File_ that contains a mapping for the blobs that make up a file (instead of storing all files as a single blob)

It doesn't do any encryption, because I don't really need it when only storing non-sensitive data for archiving purposes.
Nothing in the design would prevent adding encryption, though.

# TODOs
* add background compaction for the pack store. without this it gets really inefficient in the long run with too many small archives
* add support for deletion for commits and a tracing gc for the rest. this really only makes sense after proper compaction is implemented