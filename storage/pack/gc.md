# mark
* acquire read lock (stw?)
* get list of all currently finalized archives
* iterate over each archive, load all commits
* recurse through file tree, mark each object, skip already marked
* how to handle Has calls?
    * recursively mark object?

# sweep
* clear bitmaps after compaction