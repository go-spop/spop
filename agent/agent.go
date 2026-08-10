package agent

import (
	"net"

	"github.com/go-spop/spop/engine"
	"github.com/go-spop/spop/logger"
	"github.com/go-spop/spop/request"
	"github.com/go-spop/spop/worker"
)

func New(handler func(*request.Request), logger logger.Logger) *Agent {
	agent := &Agent{
		handler:  handler,
		logger:   logger,
		registry: engine.NewRegistry(),
	}

	return agent
}

type Agent struct {
	handler  func(*request.Request)
	logger   logger.Logger
	registry *engine.Registry
}

func (agent *Agent) Serve(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				continue
			}
			return err
		}

		go worker.Handle(engine.NewConn(conn), agent.registry, agent.handler, agent.logger)
	}
}
