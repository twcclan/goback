This is my attempt at creating a backup & archiving tool that I can use to create frequent snapshots of my game servers (though it's not limited to that).
The master branch has a somewhat working protoype. I chose to rewrite a large part of it though (see the rewrite branch for more details on this),
because I didn't quite like the way things worked out.

# Rewrite
When I first wrote the code it kind of evolved into a program that could handle local single-user backups okay-ish. However, my actual plan was to be able to have
multiple tenants send backup data to a central repository, so as to take advantage of deduplication. Also for convenience I chose to have the objects that represented
a single backup contain the full metadata tree, which would add up to quite a lot of duplicate data for large filesystem trees.

# New design
My goal is to have a design similar to how git handles its data. There are really only two differences:
* I am using protocol buffers to store the data (instead of plain text)
* I added a new object type _File_ that contains a mapping for the blobs that make up a file (instead of storing all files as a single blob)

I also plan to eventually create a storage format similar to git packfiles, so I can create large object bundles and put them into some cloud storage system.
When that is done I can set up a backup service that all my game servers can connect to (via GRPC) to send their data.