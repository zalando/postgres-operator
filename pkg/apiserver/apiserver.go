package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/pprof"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
)

const (
	httpAPITimeout  = time.Minute * 1
	shutdownTimeout = time.Second * 10
	httpReadTimeout = time.Millisecond * 100
)

// ControllerInformer describes stats methods of a controller
type controllerInformer interface {
	GetConfig() *spec.ControllerConfig
	GetOperatorConfig() *config.Config
	GetStatus() *spec.ControllerStatus
	TeamClusterList() map[string][]spec.NamespacedName
	ClusterStatus(team, namespace, cluster string) (*spec.ClusterStatus, error)
	ClusterLogs(team, namespace, cluster string) ([]*spec.LogEntry, error)
	ClusterHistory(team, namespace, cluster string) ([]*spec.Diff, error)
	ClusterDatabasesMap() map[string][]string
	WorkerLogs(workerID uint32) ([]*spec.LogEntry, error)
	ListQueue(workerID uint32) (*spec.QueueDump, error)
	GetWorkersCnt() uint32
	WorkerStatus(workerID uint32) (*spec.WorkerStatus, error)
}

// Server describes HTTP API server
type Server struct {
	logger     *logrus.Entry
	http       http.Server
	controller controllerInformer
}

var (
	clusterStatusURL     = regexp.MustCompile(`^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/(?P<namespace>[a-z0-9]([-a-z0-9]*[a-z0-9])?)/(?P<cluster>[a-zA-Z][a-zA-Z0-9-]*)/?$`)
	clusterLogsURL       = regexp.MustCompile(`^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/(?P<namespace>[a-z0-9]([-a-z0-9]*[a-z0-9])?)/(?P<cluster>[a-zA-Z][a-zA-Z0-9-]*)/logs/?$`)
	clusterHistoryURL    = regexp.MustCompile(`^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/(?P<namespace>[a-z0-9]([-a-z0-9]*[a-z0-9])?)/(?P<cluster>[a-zA-Z][a-zA-Z0-9-]*)/history/?$`)
	teamURL              = regexp.MustCompile(`^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/?$`)
	workerLogsURL        = regexp.MustCompile(`^/workers/(?P<id>\d+)/logs/?$`)
	workerEventsQueueURL = regexp.MustCompile(`^/workers/(?P<id>\d+)/queue/?$`)
	workerStatusURL      = regexp.MustCompile(`^/workers/(?P<id>\d+)/status/?$`)
	workerAllQueue       = regexp.MustCompile(`^/workers/all/queue/?$`)
	workerAllStatus      = regexp.MustCompile(`^/workers/all/status/?$`)
	clustersURL          = "/clusters/"
)

// New creates new HTTP API server
func New(controller controllerInformer, port int, logger *logrus.Logger) *Server {
	s := &Server{
		logger:     logger.WithField("pkg", "apiserver"),
		controller: controller,
	}
	mux := http.NewServeMux()

	mux.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
	mux.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
	mux.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	mux.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
	mux.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))

	mux.Handle("/status/", http.HandlerFunc(s.controllerStatus))
	mux.Handle("/config/", http.HandlerFunc(s.operatorConfig))

	mux.HandleFunc("/clusters/", s.clusters)
	mux.HandleFunc("/workers/", s.workers)
	mux.HandleFunc("/databases/", s.databases)

	s.http = http.Server{
		Addr:        fmt.Sprintf(":%d", port),
		Handler:     http.TimeoutHandler(mux, httpAPITimeout, ""),
		ReadTimeout: httpReadTimeout,
	}

	return s
}

// Run starts the HTTP server
func (s *Server) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	go func() {
		err := s.http.ListenAndServe()
		if err != http.ErrServerClosed {
			s.logger.Fatalf("Could not start http server: %v", err)
		}
	}()
	s.logger.Infof("listening on %s", s.http.Addr)

	<-stopCh

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := s.http.Shutdown(ctx)
	if err == context.DeadlineExceeded {
		s.logger.Warningf("Shutdown timeout exceeded. closing http server")
		s.http.Close()
	} else if err != nil {
		s.logger.Errorf("Could not shutdown http server: %v", err)
	}
	s.logger.Infoln("Http server shut down")
}

func (s *Server) respond(obj interface{}, err error, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}

	err = json.NewEncoder(w).Encode(obj)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		s.logger.Errorf("Could not encode: %v", err)
	}
}

func (s *Server) controllerStatus(w http.ResponseWriter, req *http.Request) {
	s.respond(s.controller.GetStatus(), nil, w)
}

func (s *Server) operatorConfig(w http.ResponseWriter, req *http.Request) {
	s.respond(map[string]interface{}{
		"controller": s.controller.GetConfig(),
		"operator":   s.controller.GetOperatorConfig(),
	}, nil, w)
}

func (s *Server) clusters(w http.ResponseWriter, req *http.Request) {
	var (
		resp interface{}
		err  error
	)

	if matches := util.FindNamedStringSubmatch(clusterStatusURL, req.URL.Path); matches != nil {
		namespace, _ := matches["namespace"]
		resp, err = s.controller.ClusterStatus(matches["team"], namespace, matches["cluster"])
	} else if matches := util.FindNamedStringSubmatch(teamURL, req.URL.Path); matches != nil {
		teamClusters := s.controller.TeamClusterList()
		clusters, found := teamClusters[matches["team"]]
		if !found {
			s.respond(nil, fmt.Errorf("could not find clusters for the team"), w)
			return
		}

		clusterNames := make([]string, 0)
		for _, cluster := range clusters {
			clusterNames = append(clusterNames, cluster.Name[len(matches["team"])+1:])
		}

		resp, err = clusterNames, nil
	} else if matches := util.FindNamedStringSubmatch(clusterLogsURL, req.URL.Path); matches != nil {
		namespace, _ := matches["namespace"]
		resp, err = s.controller.ClusterLogs(matches["team"], namespace, matches["cluster"])
	} else if matches := util.FindNamedStringSubmatch(clusterHistoryURL, req.URL.Path); matches != nil {
		namespace, _ := matches["namespace"]
		resp, err = s.controller.ClusterHistory(matches["team"], namespace, matches["cluster"])
	} else if req.URL.Path == clustersURL {
		clusterNamesPerTeam := make(map[string][]string)
		for team, clusters := range s.controller.TeamClusterList() {
			for _, cluster := range clusters {
				clusterNamesPerTeam[team] = append(clusterNamesPerTeam[team], cluster.Name[len(team)+1:])
			}
		}
		resp, err = clusterNamesPerTeam, nil
	} else {
		resp, err = nil, fmt.Errorf("page not found")
	}

	s.respond(resp, err, w)
}

func (s *Server) workers(w http.ResponseWriter, req *http.Request) {
	var (
		resp interface{}
		err  error
	)

	if workerAllQueue.MatchString(req.URL.Path) {
		s.allQueues(w, req)
		return
	} else if matches := util.FindNamedStringSubmatch(workerLogsURL, req.URL.Path); matches != nil {
		workerID, _ := strconv.Atoi(matches["id"])

		resp, err = s.controller.WorkerLogs(uint32(workerID))
	} else if matches := util.FindNamedStringSubmatch(workerEventsQueueURL, req.URL.Path); matches != nil {
		workerID, _ := strconv.Atoi(matches["id"])

		resp, err = s.controller.ListQueue(uint32(workerID))
	} else if matches := util.FindNamedStringSubmatch(workerStatusURL, req.URL.Path); matches != nil {
		var workerStatus *spec.WorkerStatus

		workerID, _ := strconv.Atoi(matches["id"])
		workerStatus, err = s.controller.WorkerStatus(uint32(workerID))
		if workerStatus == nil {
			resp = "idle"
		} else {
			resp = workerStatus
		}
	} else if workerAllStatus.MatchString(req.URL.Path) {
		s.allWorkers(w, req)
		return
	} else {
		s.respond(nil, fmt.Errorf("page not found"), w)
		return
	}

	s.respond(resp, err, w)
}

func (s *Server) databases(w http.ResponseWriter, req *http.Request) {

	databaseNamesPerCluster := s.controller.ClusterDatabasesMap()
	s.respond(databaseNamesPerCluster, nil, w)
	return

}

func (s *Server) allQueues(w http.ResponseWriter, r *http.Request) {
	workersCnt := s.controller.GetWorkersCnt()
	resp := make(map[uint32]*spec.QueueDump, workersCnt)
	for i := uint32(0); i < workersCnt; i++ {
		queueDump, err := s.controller.ListQueue(i)
		if err != nil {
			s.respond(nil, err, w)
			return
		}

		resp[i] = queueDump
	}

	s.respond(resp, nil, w)
}

func (s *Server) allWorkers(w http.ResponseWriter, r *http.Request) {
	workersCnt := s.controller.GetWorkersCnt()
	resp := make(map[uint32]interface{}, workersCnt)
	for i := uint32(0); i < workersCnt; i++ {
		status, err := s.controller.WorkerStatus(i)
		if err != nil {
			s.respond(nil, err, w)
			continue
		}

		if status == nil {
			resp[i] = "idle"
		} else {
			resp[i] = status
		}
	}

	s.respond(resp, nil, w)
}
