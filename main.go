package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/OffchainLabs/new-rollup-exploration/protocol"
	statemanager "github.com/OffchainLabs/new-rollup-exploration/state-manager"
	"github.com/OffchainLabs/new-rollup-exploration/util"
	"github.com/OffchainLabs/new-rollup-exploration/validator"
	"github.com/OffchainLabs/new-rollup-exploration/web"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/sirupsen/logrus"
)

var (
	log      = logrus.WithField("prefix", "visualizer")
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

type config struct {
	NumValidators                    uint8            `json:"num_validators"`
	NumStates                        uint64           `json:"num_states"`
	DefaultBalance                   *big.Int         `json:"initial_balance"`
	ChallengePeriod                  time.Duration    `json:"challenge_period"`
	ChallengeVertexWakeInterval      time.Duration    `json:"challenge_vertex_wake_interval"`
	DivergenceHeightByValidatorIndex map[uint8]uint64 `json:"diverge_height_by_validator_index"`
}

func defaultConfig() *config {
	defaultBalance := big.NewInt(0).Mul(protocol.Gwei, big.NewInt(100))
	return &config{
		NumValidators:               2,
		NumStates:                   10,
		DefaultBalance:              defaultBalance,
		ChallengePeriod:             time.Minute,
		ChallengeVertexWakeInterval: time.Second,
		DivergenceHeightByValidatorIndex: map[uint8]uint64{
			0: 4,
			1: 5,
			2: 6,
			3: 7,
		},
	}
}

type server struct {
	lock       sync.RWMutex
	ctx        context.Context
	cancelFn   context.CancelFunc
	cfg        *config
	port       uint
	chain      *protocol.AssertionChain
	manager    statemanager.Manager
	validators []*validator.Validator
	wsClients  map[*websocket.Conn]bool
}

func (s *server) renderConfig(c echo.Context) error {
	s.lock.RLock()
	defer s.lock.RUnlock()
	c.JSON(http.StatusOK, s.cfg)
	return nil
}

func (s *server) updateConfig(c echo.Context) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	req := defaultConfig()
	defer c.Request().Body.Close()
	enc, err := io.ReadAll(c.Request().Body)
	if err != nil {
		log.Error(err)
		// http.Error(w, "Could not read body", http.StatusBadRequest)
		return nil
	}
	if err := json.Unmarshal(enc, req); err != nil {
		log.Error(err)
		// http.Error(w, "Could not decode", http.StatusBadRequest)
		return nil
	}

	log.Info("Received update config request, restarting application...")
	// Cancel the current runtime of the application, wait a bit for cleanup,
	// then restart the application with the updated configuration.
	s.cancelFn()

	time.Sleep(2 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFn = cancel
	s.ctx = ctx
	s.cfg = req
	go s.startBackgroundRoutines(ctx, s.cfg)

	log.Info("Successfully restarted background routines")

	c.JSON(http.StatusOK, s.cfg)
	return nil
}

type assertionCreationRequest struct {
	Index uint8 `json:"index"`
}

func (s *server) triggerAssertionCreation(c echo.Context) error {
	if c.Request().Method != http.MethodPost {
		// http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return nil
	}

	req := &assertionCreationRequest{}
	defer c.Request().Body.Close()
	enc, err := io.ReadAll(c.Request().Body)
	if err != nil {
		// http.Error(w, "Could not read body", http.StatusBadRequest)
		return nil
	}
	if err := json.Unmarshal(enc, req); err != nil {
		// http.Error(w, "Could not decode", http.StatusBadRequest)
		return nil
	}
	if int(req.Index) >= len(s.validators) {
		// http.Error(w, "Validator index out of range", http.StatusBadRequest)
		return nil
	}
	s.lock.RLock()
	v := s.validators[req.Index]
	s.lock.RUnlock()
	assertion, err := v.SubmitLeafCreation(s.ctx)
	if err != nil {
		log.WithError(err).Error("Failed to create a new assertion leaf")
		// http.Error(w, "Assertion creation failed", http.StatusInternalServerError)
		return nil
	}
	c.JSON(http.StatusOK, assertion)
	return nil
}

func (s *server) triggerAssertionCreation2(index uint8) error {
	if int(index) >= len(s.validators) {
		// http.Error(w, "Validator index out of range", http.StatusBadRequest)
		return nil
	}
	s.lock.RLock()
	v := s.validators[index]
	s.lock.RUnlock()
	_, err := v.SubmitLeafCreation(s.ctx)
	if err != nil {
		log.WithError(err).Error("Failed to create a new assertion leaf")
		// http.Error(w, "Assertion creation failed", http.StatusInternalServerError)
		return nil
	}
	return nil
}

func (s *server) registerWebsocketConnection(c echo.Context) error {
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		log.Fatal(err)
	}
	s.lock.Lock()
	s.wsClients[ws] = true
	s.lock.Unlock()
	log.Info("Registered new websocket client")
	return nil
}

func (s *server) startBackgroundRoutines(ctx context.Context, cfg *config) {
	timeRef := util.NewRealTimeReference()
	validators, chain, err := initializeSystem(ctx, timeRef, cfg)
	if err != nil {
		panic(err)
	}
	s.validators = validators
	s.chain = chain
	challengeObserver := make(chan protocol.ChallengeEvent, 100)
	chainObserver := make(chan protocol.AssertionChainEvent, 100)
	s.chain.SubscribeChallengeEvents(ctx, challengeObserver)
	s.chain.SubscribeChainEvents(ctx, chainObserver)

	go s.sendChainEventsToClients(ctx, challengeObserver, chainObserver)

	for i, v := range validators {
		go v.Start(ctx)
		s.triggerAssertionCreation2(uint8(i))
	}
	log.Infof("Started application background routines successfully with config %+v", s.cfg)
}

type event struct {
	Typ           string `json:"typ"`
	Contents      string `json:"contents"`
	Vis           string `json:"vis"`
	VisChallenege string `json:"visChallenge"`
}

func (s *server) sendChainEventsToClients(
	ctx context.Context,
	chalEvs <-chan protocol.ChallengeEvent,
	chainEvs <-chan protocol.AssertionChainEvent,
) {
	for {
		select {
		case ev := <-chalEvs:
			log.Infof("Got challenge event: %+T, and %+v", ev, ev)
			vis := s.chain.Visualize()
			visChallenge := s.chain.VisualizeChallenges()
			s.lock.RLock()
			eventToSend := &event{
				Typ:           fmt.Sprintf("%+T", ev),
				Contents:      fmt.Sprintf("%+v", ev),
				Vis:           vis,
				VisChallenege: visChallenge,
			}
			enc, err := json.Marshal(eventToSend)
			if err != nil {
				log.Error(err)
				continue
			}

			// send to every client that is currently connected
			for client := range s.wsClients {
				err := client.WriteMessage(websocket.TextMessage, enc)
				if err != nil {
					log.Errorf("Websocket error: %s", err)
					client.Close()
					delete(s.wsClients, client)
				}
			}
			s.lock.RUnlock()
		case ev := <-chainEvs:
			log.Infof("Got chain event: %+T, and %+v", ev, ev)
			vis := s.chain.Visualize()
			visChallenge := s.chain.VisualizeChallenges()
			s.lock.RLock()
			eventToSend := &event{
				Typ:           fmt.Sprintf("%+T", ev),
				Contents:      fmt.Sprintf("%+v", ev),
				Vis:           vis,
				VisChallenege: visChallenge,
			}
			enc, err := json.Marshal(eventToSend)
			if err != nil {
				log.Error(err)
				continue
			}

			// send to every client that is currently connected
			for client := range s.wsClients {
				err := client.WriteMessage(websocket.TextMessage, enc)
				if err != nil {
					log.Errorf("Websocket error: %s", err)
					client.Close()
					delete(s.wsClients, client)
				}
			}
			s.lock.RUnlock()
		case <-ctx.Done():
			return
		default:
		}
	}
}

// Registers all of the server's routes for the web application.
func (s *server) registerRoutes(e *echo.Echo) {
	// Register the frontend website static assets including HTML.
	web.RegisterHandlers(e)

	// Handle websocket connection registration.
	e.GET("/api/ws", s.registerWebsocketConnection)

	// Configuration related-handlers, either reading the config
	// or updating the config and restarting the application.
	e.GET("/api/config", s.renderConfig)
	e.POST("/api/config", s.updateConfig)

	// API triggers of validator actions, such as leaf creation at a validator's
	// latest height via the web app.
	e.POST("/api/assertions", s.triggerAssertionCreation)
}

func initializeSystem(
	ctx context.Context,
	timeRef util.TimeReference,
	cfg *config,
) ([]*validator.Validator, *protocol.AssertionChain, error) {
	chain := protocol.NewAssertionChain(ctx, timeRef, cfg.ChallengePeriod)

	validatorAddrs := make([]common.Address, cfg.NumValidators)
	for i := uint8(0); i < cfg.NumValidators; i++ {
		// Make the addrs 1-indexed so that we don't use the zero address.
		validatorAddrs[i] = common.BytesToAddress([]byte{i + 1})
	}

	// Increase the balance for each validator in the test.
	bal := big.NewInt(0).Mul(protocol.Gwei, big.NewInt(100))
	err := chain.Tx(func(tx *protocol.ActiveTx, p protocol.OnChainProtocol) error {
		for _, addr := range validatorAddrs {
			chain.AddToBalance(tx, addr, bal)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Initialize each validator associated state roots which diverge
	// at specified points in the test config.
	validatorStateRoots := make([][]common.Hash, cfg.NumValidators)
	for i := uint8(0); i < cfg.NumValidators; i++ {
		divergenceHeight := cfg.DivergenceHeightByValidatorIndex[i]
		stateRoots := make([]common.Hash, cfg.NumStates)
		for i := uint64(0); i < cfg.NumStates; i++ {
			if divergenceHeight == 0 || i < divergenceHeight {
				stateRoots[i] = util.HashForUint(i)
			} else {
				divergingRoot := make([]byte, 32)
				_, err = rand.Read(divergingRoot)
				if err != nil {
					return nil, nil, err
				}
				stateRoots[i] = common.BytesToHash(divergingRoot)
			}
		}
		validatorStateRoots[i] = stateRoots
	}

	// Initialize each validator.
	validators := make([]*validator.Validator, cfg.NumValidators)
	for i := 0; i < len(validators); i++ {
		manager := statemanager.New(validatorStateRoots[i])
		addr := validatorAddrs[i]
		v, valErr := validator.New(
			ctx,
			chain,
			manager,
			validator.WithName(fmt.Sprintf("%d", i)),
			validator.WithAddress(addr),
			validator.WithDisableLeafCreation(),
			validator.WithTimeReference(timeRef),
			validator.WithChallengeVertexWakeInterval(cfg.ChallengeVertexWakeInterval),
		)
		if valErr != nil {
			return nil, nil, valErr
		}
		validators[i] = v
	}
	return validators, chain, nil
}

// Initializes a server that is able to start validators, trigger
// validator events, and provides data on their events via websocket connections.
func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := defaultConfig()
	s := &server{
		ctx:       ctx,
		cancelFn:  cancel,
		cfg:       cfg,
		port:      8000,
		wsClients: map[*websocket.Conn]bool{},
	}

	e := echo.New()
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogStatus: true,
		LogValuesFunc: func(c echo.Context, values middleware.RequestLoggerValues) error {
			return nil
		},
	}))

	// Register all the server routes for the application.
	s.registerRoutes(e)

	// Start the main application routines in the background.
	go s.startBackgroundRoutines(ctx, cfg)

	// Listen and serve the web application.
	log.Infof("Server listening on port %d", s.port)
	if err := e.Start(fmt.Sprintf(":%d", s.port)); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
