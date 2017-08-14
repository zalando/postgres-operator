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
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
)

const (
	httpAPITimeout  = time.Minute * 1
	shutdownTimeout = time.Second * 10
	httpReadTimeout = time.Millisecond * 100
)

// ControllerInformer describes stats methods of a controller
type ControllerInformer interface {
	GetConfig() *spec.ControllerConfig
	GetOperatorConfig() *config.Config
	GetStatus() *spec.ControllerStatus
	TeamClusterList() map[string][]spec.NamespacedName
	ClusterStatus(team, cluster string) (*spec.ClusterStatus, error)
	ClusterLogs(team, cluster string) ([]*spec.LogEntry, error)
	WorkerLogs(workerID uint32) ([]*spec.LogEntry, error)
	ListQueue(workerID uint32) (*spec.QueueDump, error)
	GetWorkersCnt() uint32
}

// Server describes HTTP API server
type Server struct {
	logger     *logrus.Entry
	http       http.Server
	controller ControllerInformer
}

var (
	clusterStatusURL     = regexp.MustCompile(`^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/(?P<cluster>[a-zA-Z][a-zA-Z0-9]*)/?$`)
	clusterLogsURL       = regexp.MustCompile(`^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/(?P<cluster>[a-zA-Z][a-zA-Z0-9]*)/logs/?$`)
	teamURL              = regexp.MustCompile(`^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/?$`)
	workerLogsURL        = regexp.MustCompile(`^/workers/(?P<id>\d+)/logs/?$`)
	workerEventsQueueURL = regexp.MustCompile(`^/workers/(?P<id>\d+)/queue/?$`)
	workerAllQueue       = regexp.MustCompile(`^/workers/all/queue/?$`)
	clustersURL          = "/clusters/"
)

// New creates new HTTP API server
func New(controller ControllerInformer, port int, logger *logrus.Logger) *Server {
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
	s.logger.Infof("Listening on %s", s.http.Addr)

	<-stopCh

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := s.http.Shutdown(ctx)
	if err == context.DeadlineExceeded {
		s.logger.Warnf("Shutdown timeout exceeded. closing http server")
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

	if matches := clusterStatusURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
		resp, err = s.controller.ClusterStatus(matches[0][1], matches[0][2])
	} else if matches := teamURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
		teamClusters := s.controller.TeamClusterList()
		clusters, found := teamClusters[matches[0][1]]
		if !found {
			s.respond(nil, fmt.Errorf("could not find clusters for the team"), w)
		}

		clusterNames := make([]string, 0)
		for _, cluster := range clusters {
			clusterNames = append(clusterNames, cluster.Name[len(matches[0][1])+1:])
		}

		s.respond(clusterNames, nil, w)
		return
	} else if matches := clusterLogsURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
		resp, err = s.controller.ClusterLogs(matches[0][1], matches[0][2])
	} else if req.URL.Path == clustersURL {
		res := make(map[string][]string)
		for team, clusters := range s.controller.TeamClusterList() {
			for _, cluster := range clusters {
				res[team] = append(res[team], cluster.Name[len(team)+1:])
			}
		}

		s.respond(res, nil, w)
		return
	} else {
		s.respond(nil, fmt.Errorf("page not found"), w)
		return
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
	} else if matches := workerLogsURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
		workerID, _ := strconv.Atoi(matches[0][1])

		resp, err = s.controller.WorkerLogs(uint32(workerID))
	} else if matches := workerEventsQueueURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
		workerID, _ := strconv.Atoi(matches[0][1])

		resp, err = s.controller.ListQueue(uint32(workerID))
	} else {
		s.respond(nil, fmt.Errorf("page not found"), w)
		return
	}

	s.respond(resp, err, w)
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
