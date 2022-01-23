package pack

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	GetObjectSize       = stats.Int64("goback.io/storage/pack/get_object_size", "size of objects retrieved", stats.UnitBytes)
	PutObjectSize       = stats.Int64("goback.io/storage/pack/put_object_size", "size of objects stored", stats.UnitBytes)
	ArchiveReadLatency  = stats.Float64("goback.io/storage/pack/archive_read_latency", "duration of archiveWriter reads", stats.UnitMilliseconds)
	ArchiveWriteLatency = stats.Float64("goback.io/storage/pack/archive_write_latency", "duration of archiveWriter writes", stats.UnitMilliseconds)
	ArchiveReadSize     = stats.Int64("goback.io/storage/pack/archive_read_size", "size of archiveWriter reads", stats.UnitBytes)
	ArchiveWriteSize    = stats.Int64("goback.io/storage/pack/archive_write_size", "size of archiveWriter writes", stats.UnitBytes)
	TotalLiveObjects    = stats.Int64("goback.io/storage/pack/total_live_objects", "number of live objects before", stats.UnitDimensionless)

	GCMarkTime          = stats.Float64("goback.io/storage/pack/gc_mark_time", "duration of gc mark runs", stats.UnitMilliseconds)
	GCObjectsScanned    = stats.Int64("goback.io/storage/pack/gc_objects_scanned", "total number of objects scanned durign gc run", stats.UnitDimensionless)
	GCCommitMarkLatency = stats.Float64("goback.io/storage/pack/gc_commit_mark_latency", "time it took to recursively mark a commit", stats.UnitMilliseconds)

	KeyObjectType, _ = tag.NewKey("object_type")

	DefaultBytesDistribution        = view.Distribution(0, 256, 512, 1024, 2048, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216)
	DefaultMillisecondsDistribution = view.Distribution(0, 0.01, 0.05, 0.1, 0.3, 0.6, 0.8, 1, 2, 3, 4, 5, 6, 8, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80, 100, 130, 160, 200, 250, 300, 400, 500, 650, 800, 1000, 2000, 5000, 10000)

	GetObjectSizeView = &view.View{
		Name:        "goback.io/storage/pack/get_object_size",
		Description: "size of objects retrieved",
		Measure:     GetObjectSize,
		Aggregation: DefaultBytesDistribution,
		TagKeys:     []tag.Key{KeyObjectType},
	}

	ObjectsRetrievedView = &view.View{
		Name:        "goback.io/storage/pack/get_object_count",
		Description: "number of objects retrieved",
		Measure:     GetObjectSize,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{KeyObjectType},
	}

	PutObjectSizeView = &view.View{
		Name:        "goback.io/storage/pack/put_object_size",
		Description: "size of objects stored",
		Measure:     PutObjectSize,
		Aggregation: DefaultBytesDistribution,
		TagKeys:     []tag.Key{KeyObjectType},
	}

	ObjectsStoredView = &view.View{
		Name:        "goback.io/storage/pack/put_object_count",
		Description: "number of objects stored",
		Measure:     PutObjectSize,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{KeyObjectType},
	}

	ArchiveReadLatencyView = &view.View{
		Name:        "goback.io/storage/pack/archive_read_latency",
		Description: "duration of archiveWriter reads",
		Measure:     ArchiveReadLatency,
		Aggregation: DefaultMillisecondsDistribution,
		TagKeys:     []tag.Key{KeyObjectType},
	}

	ArchiveWriteLatencyView = &view.View{
		Name:        "goback.io/storage/pack/archive_write_latency",
		Description: "duration of archiveWriter writes",
		Measure:     ArchiveWriteLatency,
		Aggregation: DefaultMillisecondsDistribution,
		TagKeys:     []tag.Key{KeyObjectType},
	}

	ArchiveReadSizeView = &view.View{
		Name:        "goback.io/storage/pack/archive_read_size",
		Description: "size of archiveWriter reads",
		Measure:     ArchiveReadSize,
		Aggregation: DefaultBytesDistribution,
		TagKeys:     []tag.Key{KeyObjectType},
	}

	ArchiveWriteSizeView = &view.View{
		Name:        "goback.io/storage/pack/archive_write_size",
		Description: "duration of archiveWriter writes",
		Measure:     ArchiveWriteSize,
		Aggregation: DefaultBytesDistribution,
		TagKeys:     []tag.Key{KeyObjectType},
	}

	TotalLiveObjectsView = &view.View{
		Name:        "goback.io/storage/pack/total_live_objects",
		Description: "total number of live objects",
		Measure:     TotalLiveObjects,
		Aggregation: view.LastValue(),
	}

	GCMarkTimeView = &view.View{
		Name:        "goback.io/storage/pack/gc_mark_time",
		Description: "duration of GC mark runs",
		Measure:     GCMarkTime,
		Aggregation: DefaultMillisecondsDistribution,
	}

	GCObjectsScannedView = &view.View{
		Name:        "goback.io/storage/pack/gc_objects_scanned",
		Description: "number of objects scanned during GC runs",
		Measure:     GCObjectsScanned,
		Aggregation: view.Count(),
	}

	GCCommitMarkLatencyView = &view.View{
		Name:        "goback.io/storage/pack/gc_commit_mark_latency",
		Description: "time it took to recursively mark a commit",
		Measure:     GCCommitMarkLatency,
		Aggregation: DefaultMillisecondsDistribution,
	}

	GCCommitMarkCountView = &view.View{
		Name:        "goback.io/storage/pack/gc_commit_mark_count",
		Description: "number of commits marked",
		Measure:     GCCommitMarkLatency,
		Aggregation: view.Count(),
	}

	DefaultViews = []*view.View{
		ArchiveReadLatencyView,
		ArchiveReadSizeView,
		ArchiveWriteLatencyView,
		ArchiveWriteSizeView,
		ObjectsRetrievedView,
		ObjectsStoredView,
		PutObjectSizeView,
		GetObjectSizeView,
		TotalLiveObjectsView,
		GCMarkTimeView,
		GCObjectsScannedView,
		GCCommitMarkLatencyView,
		GCCommitMarkCountView,
	}
)
