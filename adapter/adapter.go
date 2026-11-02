package adapter

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	artcfg "github.com/fluxcd/pkg/artifact/config"
	intdigest "github.com/fluxcd/pkg/artifact/digest"
	artstore "github.com/fluxcd/pkg/artifact/storage"
	helper "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/events"
	"github.com/fluxcd/source-controller/internal/cache"
	"github.com/fluxcd/source-controller/internal/controller"
	"helm.sh/helm/v4/pkg/getter"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/fluxcd/source-controller/api/v1beta2"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
	getters  = getter.Providers{
		getter.Provider{
			Schemes: []string{"http", "https"},
			New:     getter.NewHTTPGetter,
		},
		getter.Provider{
			Schemes: []string{"oci"},
			New:     getter.NewOCIGetter,
		},
	}
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1beta2.AddToScheme(scheme))
	utilruntime.Must(v1.AddToScheme(scheme))
}

type SourceAdapter struct {
	Context           context.Context
	StoragePath       string
	FileServerPort    int
	ControllerName    string
	ReconcilerOptions ReconcilerOptions
	MetricOptions     helper.Metrics
	LeaderElection    *bool
}
type ReconcilerOptions struct {
	RateLimiter               workqueue.TypedRateLimiter[reconcile.Request]
	DependencyRequeueInterval time.Duration
}

func SetupSourceReconcilers(mgr ctrl.Manager, adapter SourceAdapter) error {
	storage := mustInitStorage(adapter.StoragePath, adapter.getFileServerAddress())
	cacheRecorder := cache.MustMakeMetrics()

	helmIndexCache, helmIndexCacheItemTTL := mustInitHelmCache(0, "15m", "1m")
	eventRecorder, err := events.NewRecorder(ctrl.Log, "", adapter.ControllerName, events.WithManager(mgr))
	if err != nil {
		return err
	}

	if err := (&controller.HelmRepositoryReconciler{
		Client:         mgr.GetClient(),
		Recorder:       eventRecorder,
		Metrics:        adapter.MetricOptions,
		Storage:        storage,
		Getters:        getters,
		ControllerName: adapter.ControllerName,
		Cache:          helmIndexCache,
		TTL:            helmIndexCacheItemTTL,
		CacheRecorder:  cacheRecorder,
		LeaderElection: adapter.LeaderElection,
	}).SetupWithManager(mgr, controller.HelmRepositoryReconcilerOptions{
		RateLimiter: adapter.ReconcilerOptions.RateLimiter,
	}); err != nil {
		return err
	}

	if err := (&controller.GitRepositoryReconciler{
		Client:         mgr.GetClient(),
		Recorder:       eventRecorder,
		Metrics:        adapter.MetricOptions,
		Storage:        storage,
		ControllerName: adapter.ControllerName,
		LeaderElection: adapter.LeaderElection,
	}).SetupWithManager(mgr, controller.GitRepositoryReconcilerOptions{
		DependencyRequeueInterval: adapter.ReconcilerOptions.DependencyRequeueInterval,
		RateLimiter:               adapter.ReconcilerOptions.RateLimiter,
	}); err != nil {
		return err
	}

	if err := (&controller.BucketReconciler{
		Client:         mgr.GetClient(),
		Recorder:       eventRecorder,
		Metrics:        adapter.MetricOptions,
		Storage:        storage,
		ControllerName: adapter.ControllerName,
		LeaderElection: adapter.LeaderElection,
	}).SetupWithManager(mgr, controller.BucketReconcilerOptions{
		RateLimiter: adapter.ReconcilerOptions.RateLimiter,
	}); err != nil {
		return err
	}

	if err := (&controller.HelmChartReconciler{
		Client:         mgr.GetClient(),
		Storage:        storage,
		Getters:        getters,
		Recorder:       eventRecorder,
		Metrics:        adapter.MetricOptions,
		ControllerName: adapter.ControllerName,
		Cache:          helmIndexCache,
		TTL:            helmIndexCacheItemTTL,
		CacheRecorder:  cacheRecorder,
		LeaderElection: adapter.LeaderElection,
	}).SetupWithManager(adapter.Context, mgr, controller.HelmChartReconcilerOptions{
		RateLimiter: adapter.ReconcilerOptions.RateLimiter,
	}); err != nil {
		return err
	}

	go func() {
		<-mgr.Elected()
		startFileServer(storage.BasePath, adapter.getFileServerAddress())
	}()
	return nil
}

func (a *SourceAdapter) getFileServerAddress() string {
	port := 9090
	if a.FileServerPort != 0 {
		port = a.FileServerPort
	}
	return fmt.Sprintf(":%d", port)
}

func mustInitStorage(path string, storageAdvAddr string) *artstore.Storage {
	if storageAdvAddr == "" {
		storageAdvAddr = determineAdvStorageAddr(storageAdvAddr)
	}

	opts := artcfg.Options{
		StoragePath:              path,
		StorageAdvAddress:        storageAdvAddr,
		ArtifactRetentionTTL:     60 * time.Second,
		ArtifactRetentionRecords: 2,
		ArtifactDigestAlgo:       intdigest.Canonical.String(),
	}

	storage, err := artstore.New(&opts)
	if err != nil {
		setupLog.Error(err, "unable to initialise storage")
		os.Exit(1)
	}
	return storage
}

func mustInitHelmCache(maxSize int, itemTTL, purgeInterval string) (*cache.Cache, time.Duration) {
	if maxSize <= 0 {
		setupLog.Info("caching of Helm index files is disabled")
		return nil, -1
	}

	interval, err := time.ParseDuration(purgeInterval)
	if err != nil {
		setupLog.Error(err, "unable to parse Helm index cache purge interval")
		os.Exit(1)
	}

	ttl, err := time.ParseDuration(itemTTL)
	if err != nil {
		setupLog.Error(err, "unable to parse Helm index cache item TTL")
		os.Exit(1)
	}

	return cache.New(maxSize, interval), ttl
}

func determineAdvStorageAddr(storageAddr string) string {
	host, port, err := net.SplitHostPort(storageAddr)
	if err != nil {
		setupLog.Error(err, "unable to parse storage address")
		os.Exit(1)
	}
	switch host {
	case "":
		host = "localhost"
	case "0.0.0.0":
		host = os.Getenv("HOSTNAME")
		if host == "" {
			hn, err := os.Hostname()
			if err != nil {
				setupLog.Error(err, "0.0.0.0 specified in storage addr but hostname is invalid")
				os.Exit(1)
			}
			host = hn
		}
	}
	return net.JoinHostPort(host, port)
}

func startFileServer(path string, address string) {
	setupLog.Info("starting file server")
	fs := http.FileServer(http.Dir(path))
	mux := http.NewServeMux()
	mux.Handle("/", fs)
	err := http.ListenAndServe(address, mux)
	if err != nil {
		setupLog.Error(err, "file server error")
	}
}
