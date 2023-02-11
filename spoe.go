package spoe

import (
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/net/netutil"
)

const (
	version      = "2.0"
	maxFrameSize = 16380
)

type Handler func(msgs *MessageIterator) ([]Action, error)

type Config struct {
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	MaxConnections int
}

var defaultConfig = Config{
	ReadTimeout:    time.Second,
	WriteTimeout:   time.Second,
	IdleTimeout:    30 * time.Second,
	MaxConnections: 0,
}

type EngKey struct {
	FrameSize int
	Engine    string
	Conn      net.Conn
}

type Agent struct {
	Handler Handler
	cfg     Config
	log     Logger

	maxFrameSize int

	engLock sync.Mutex
	engines map[EngKey]*Engine
}

type Opt func(agent *Agent)

func WithLogger(l Logger) Opt {
	return func(agent *Agent) {
		agent.log = l
	}
}

func New(h Handler, opts ...Opt) *Agent {
	return NewWithConfig(h, defaultConfig, opts...)
}

type Engine struct {
	frames chan Frame
	count  int32
}

func NewWithConfig(h Handler, cfg Config, opts ...Opt) *Agent {
	agent := &Agent{
		Handler: h,
		cfg:     cfg,
		engines: make(map[EngKey]*Engine),
	}

	for _, opt := range opts {
		opt(agent)
	}

	if agent.log == nil {
		agent.log = &nillogger{}
	}

	return agent
}

func (a *Agent) ListenAndServe(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "spoe")
	}
	defer lis.Close()
	if a.cfg.MaxConnections > 0 {
		a.log.Infof("spoe: max connections: %d", a.cfg.MaxConnections)
		lis = netutil.LimitListener(lis, a.cfg.MaxConnections)
	}

	return a.Serve(lis)
}

func (a *Agent) Serve(lis net.Listener) error {
	a.log.Infof("spoe: listening on %s", lis.Addr().String())

	for {
		c, err := lis.Accept()
		if err != nil {
			return err
		}

		if tcp, ok := c.(*net.TCPConn); ok {
			err = tcp.SetWriteBuffer(maxFrameSize * 4)
			if err != nil {
				return err
			}
			err = tcp.SetReadBuffer(maxFrameSize * 4)
			if err != nil {
				return err
			}
		}

		a.log.Debugf("spoe: connection from %s", c.RemoteAddr())

		go func() {
			spoeconn := &conn{
				Conn:    c,
				handler: a.Handler,
				cfg:     a.cfg,
				log:     a.log,

				notifyTasks: make(chan Frame),
			}

			err := spoeconn.run(a)
			if err != nil {
				a.log.Warnf("spoe: error handling connection: %s", err)
			}
		}()
	}
}
