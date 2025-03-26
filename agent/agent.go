package agent

import (
	"net"

	"github.com/go-spoe/spoe/logger"
	"github.com/go-spoe/spoe/request"
	"github.com/go-spoe/spoe/worker"
)

func New(handler func(*request.Request), logger logger.Logger) *Agent {
	agent := &Agent{
		handler: handler,
		logger:  logger,
	}

	return agent
}

type Agent struct {
	handler func(*request.Request)
	logger  logger.Logger
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

		go worker.Handle(conn, agent.handler, agent.logger)
	}
}
