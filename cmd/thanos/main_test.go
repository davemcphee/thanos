package main

import (
	"context"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"github.com/thanos-io/thanos/pkg/block/metadata"

	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/oklog/ulid"
	"github.com/prometheus/client_golang/prometheus"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/tsdb/labels"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/compact"
	"github.com/thanos-io/thanos/pkg/compact/downsample"
	"github.com/thanos-io/thanos/pkg/objstore/inmem"
	"github.com/thanos-io/thanos/pkg/testutil"
)

func TestCleanupIndexCacheFolder(t *testing.T) {
	logger := log.NewLogfmtLogger(os.Stderr)
	dir, err := ioutil.TempDir("", "test-compact-cleanup")
	testutil.Ok(t, err)
	defer func() { testutil.Ok(t, os.RemoveAll(dir)) }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bkt := inmem.NewBucket()

	// Upload one compaction lvl = 2 block, one compaction lvl = 1.
	// We generate index cache files only for lvl > 1 blocks.
	{
		id, err := testutil.CreateBlock(
			ctx,
			dir,
			[]labels.Labels{{{Name: "a", Value: "1"}}},
			1, 0, downsample.DownsampleRange0+1, // Pass the minimum DownsampleRange0 check.
			labels.Labels{{Name: "e1", Value: "1"}},
			downsample.ResLevel0)
		testutil.Ok(t, err)

		meta, err := metadata.Read(filepath.Join(dir, id.String()))
		testutil.Ok(t, err)

		meta.Compaction.Level = 2

		testutil.Ok(t, metadata.Write(logger, filepath.Join(dir, id.String()), meta))
		testutil.Ok(t, block.Upload(ctx, logger, bkt, path.Join(dir, id.String())))
	}
	{
		id, err := testutil.CreateBlock(
			ctx,
			dir,
			[]labels.Labels{{{Name: "a", Value: "1"}}},
			1, 0, downsample.DownsampleRange0+1, // Pass the minimum DownsampleRange0 check.
			labels.Labels{{Name: "e1", Value: "1"}},
			downsample.ResLevel0)
		testutil.Ok(t, err)
		testutil.Ok(t, block.Upload(ctx, logger, bkt, path.Join(dir, id.String())))
	}

	reg := prometheus.NewRegistry()
	expReg := prometheus.NewRegistry()
	genIndexExp := prometheus.NewCounter(prometheus.CounterOpts{
		Name: metricIndexGenerateName,
		Help: metricIndexGenerateHelp,
	})
	expReg.MustRegister(genIndexExp)

	testutil.Ok(t, genMissingIndexCacheFiles(ctx, logger, reg, bkt, dir))

	genIndexExp.Inc()
	testutil.GatherAndCompare(t, expReg, reg, metricIndexGenerateName)

	_, err = os.Stat(dir)
	testutil.Assert(t, os.IsNotExist(err), "index cache dir shouldn't not exist at the end of execution")
}

func TestCleanupDownsampleCacheFolder(t *testing.T) {
	logger := log.NewLogfmtLogger(os.Stderr)
	dir, err := ioutil.TempDir("", "test-compact-cleanup")
	testutil.Ok(t, err)
	defer func() { testutil.Ok(t, os.RemoveAll(dir)) }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bkt := inmem.NewBucket()
	var id ulid.ULID
	{
		id, err = testutil.CreateBlock(
			ctx,
			dir,
			[]labels.Labels{{{Name: "a", Value: "1"}}},
			1, 0, downsample.DownsampleRange0+1, // Pass the minimum DownsampleRange0 check.
			labels.Labels{{Name: "e1", Value: "1"}},
			downsample.ResLevel0)
		testutil.Ok(t, err)
		testutil.Ok(t, block.Upload(ctx, logger, bkt, path.Join(dir, id.String())))
	}

	meta, err := block.DownloadMeta(ctx, logger, bkt, id)
	testutil.Ok(t, err)

	metrics := newDownsampleMetrics(prometheus.NewRegistry())
	testutil.Equals(t, 0.0, promtest.ToFloat64(metrics.downsamples.WithLabelValues(compact.GroupKey(meta.Thanos))))
	testutil.Ok(t, downsampleBucket(ctx, logger, metrics, bkt, dir))
	testutil.Equals(t, 1.0, promtest.ToFloat64(metrics.downsamples.WithLabelValues(compact.GroupKey(meta.Thanos))))

	_, err = os.Stat(dir)
	testutil.Assert(t, os.IsNotExist(err), "index cache dir shouldn't not exist at the end of execution")
}
