package validator

import (
	"context"
	"fmt"
	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	"github.com/OffchainLabs/challenge-protocol-v2/protocol/sol-implementation"
	"github.com/OffchainLabs/challenge-protocol-v2/state-manager"
	"github.com/OffchainLabs/challenge-protocol-v2/util"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"time"
)

var (
	ErrConfirmed          = errors.New("vertex has been confirmed")
	ErrSiblingConfirmed   = errors.New("vertex sibling has been confirmed")
	ErrPrevNone           = errors.New("vertex parent is none")
	ErrChallengeCompleted = errors.New("challenge has been completed")
)

type vertexTrackerConfig struct {
	actEveryNSeconds      time.Duration
	timeRef               util.TimeReference
	challengePeriodLength time.Duration
	challengeCreationTime time.Time
	chain                 protocol.Protocol
	stateManager          statemanager.Manager
	validatorName         string
	validatorAddress      common.Address
}

type vertexTracker struct {
	cfg       *vertexTrackerConfig
	challenge protocol.Challenge
	vertex    protocol.ChallengeVertex
	fsm       *util.Fsm[vertexTrackerAction, vertexTrackerState]
}

func newVertexTracker(
	cfg *vertexTrackerConfig,
	challenge protocol.Challenge,
	vertex protocol.ChallengeVertex,
) (*vertexTracker, error) {
	fsm, err := newVertexTrackerFsm(trackerStarted)
	if err != nil {
		return nil, err
	}
	return &vertexTracker{
		cfg:       cfg,
		challenge: challenge,
		vertex:    vertex,
		fsm:       fsm,
	}, nil
}

func (v *vertexTracker) spawn(ctx context.Context, tx protocol.ActiveTx) {
	commitment, err := v.vertex.HistoryCommitment(ctx, tx)
	if err != nil {
		return
	}
	log.WithFields(logrus.Fields{
		"height":        commitment.Height,
		"merkle":        util.Trunc(commitment.Merkle[:]),
		"validatorName": v.cfg.validatorName,
	}).Info("Tracking challenge vertex")

	t := v.cfg.timeRef.NewTicker(v.cfg.actEveryNSeconds)
	defer t.Stop()
	for {
		select {
		case <-t.C():
			// Check if the associated vertex or challenge are confirmed,
			// or if a rival vertex exists that has been confirmed before acting.
			shouldComplete, err := v.trackerShouldComplete(ctx)
			if err != nil {
				log.Error(err)
				continue
			}
			if shouldComplete {
				log.WithFields(logrus.Fields{
					"height":        commitment.Height,
					"merkle":        util.Trunc(commitment.Merkle[:]),
					"validatorName": v.cfg.validatorName,
				}).Debug("Vertex tracker received notice of a confirmation, exiting")
				return
			}
			if err := v.act(ctx); err != nil {
				log.Error(err)
			}
		case <-ctx.Done():
			log.WithFields(logrus.Fields{
				"height": commitment.Height,
				"merkle": util.Trunc(commitment.Merkle[:]),
			}).Debug("Challenge goroutine exiting")
			return
		}
	}
}

func (vt *vertexTracker) trackerShouldComplete(ctx context.Context) (bool, error) {
	var challengeCompleted bool
	var siblingConfirmed bool
	var err error
	if err = vt.cfg.chain.Call(func(tx protocol.ActiveTx) error {
		challengeCompleted, err = vt.challenge.Completed(ctx, tx)
		if err != nil {
			return nil
		}
		siblingConfirmed, err = vt.vertex.HasConfirmedSibling(ctx, tx)
		if err != nil {
			return nil
		}
		return nil
	}); err != nil {
		return false, err
	}
	return challengeCompleted || siblingConfirmed, nil
}

func (vt *vertexTracker) act(ctx context.Context) error {
	current := vt.fsm.Current()
	switch current.State {
	case trackerStarted:
		prevVertex, err := vt.prevVertex(ctx)
		if err != nil {
			return err
		}
		atOneStepFork, err := vt.checkOneStepFork(ctx, prevVertex)
		if err != nil {
			return err
		}
		isPresumptive, err := vt.isPresumptive(ctx)
		if err != nil {
			return err
		}
		if atOneStepFork {
			return vt.fsm.Do(actOneStepFork{
				forkPointVertex: prevVertex,
			})
		}
		if isPresumptive {
			return vt.fsm.Do(markPresumptive{})
		}
		return vt.fsm.Do(bisect{})
	case trackerAtOneStepFork:
		event, ok := current.SourceEvent.(actOneStepFork)
		if !ok {
			return fmt.Errorf("bad source event: %s", event)
		}
		log.WithField("name", vt.cfg.validatorName).Infof(
			"Reached one-step-fork at %d and commitment %s",
			event.forkPointVertex.HistoryCommitment().Height,
			util.Trunc(event.forkPointVertex.HistoryCommitment().Merkle.Bytes()),
		)
		if vt.challenge.GetType() == protocol.SmallStepChallenge {
			return vt.fsm.Do(actOneStepProof{})
		}
		return vt.fsm.Do(openSubchallenge{})
	case trackerAtOneStepProof:
		log.Info("Checking one-step-proof against protocol")
		return vt.fsm.Do(actOneStepProof{})
	case trackerOpeningSubchallenge:
		// TODO: Implement.
		return vt.fsm.Do(openSubchallenge{})
	case trackerAddingSubchallengeLeaf:
		// TODO: Implement.
		return vt.fsm.Do(openSubchallengeLeaf{})
	case trackerBisecting:
		// TODO: Seems to allow for double bisections?
		bisectedTo, err := vt.bisect(ctx, vt.vertex)
		if err != nil {
			if errors.Is(err, solimpl.ErrAlreadyExists) {
				return vt.fsm.Do(merge{})
			}
			return err
		}
		tracker, err := newVertexTracker(
			vt.cfg,
			vt.challenge,
			bisectedTo,
		)
		if err != nil {
			return err
		}
		go tracker.spawn(ctx)
		return vt.fsm.Do(backToStart{})
	case trackerMerging:
		mergedTo, err := vt.mergeToExistingVertex(ctx)
		if err != nil {
			return err
		}
		tracker, err := newVertexTracker(
			vt.cfg,
			vt.challenge,
			mergedTo,
		)
		if err != nil {
			return err
		}
		go tracker.spawn(ctx)

		// TODO: This seems wrong...what to do?
		return vt.fsm.Do(backToStart{})
	case trackerConfirming:
		// TODO: Implement.
		return vt.fsm.Do(confirmWinner{})
	case trackerPresumptive:
		// Terminal state does nothing. The vertex tracker will end next time it acts.
		isPs, err := vt.isPresumptive(ctx)
		if err != nil {
			return err
		}
		if !isPs {
			return vt.fsm.Do(backToStart{})
		}
		return vt.fsm.Do(markPresumptive{})
	case trackerAwaitingSubchallengeResolution:
		// Terminal state does nothing. The vertex tracker will end next time it acts.
		return vt.fsm.Do(awaitSubchallengeResolution{})
	default:
		return fmt.Errorf("invalid state: %s", current.State)
	}
}

func (vt *vertexTracker) isPresumptive(ctx context.Context) (bool, error) {
	var isPresumptive bool
	if err := vt.cfg.chain.Call(func(tx protocol.ActiveTx) error {
		ps, fetchErr := vt.vertex.IsPresumptiveSuccessor(ctx, tx)
		if fetchErr != nil {
			return fetchErr
		}
		isPresumptive = ps
		return nil
	}); err != nil {
		return false, err
	}
	return isPresumptive, nil
}

func (vt *vertexTracker) checkOneStepFork(ctx context.Context, prevVertex protocol.ChallengeVertex) (bool, error) {
	commitment := vt.vertex.HistoryCommitment()
	prevCommitment := prevVertex.HistoryCommitment()
	if commitment.Height != prevCommitment.Height+1 {
		return false, nil
	}
	var oneStepFork bool
	if err := vt.cfg.chain.Call(func(tx protocol.ActiveTx) error {
		atOneStepFork, fetchErr := prevVertex.ChildrenAreAtOneStepFork(ctx, tx)
		if fetchErr != nil {
			return fetchErr
		}
		oneStepFork = atOneStepFork
		return nil
	}); err != nil {
		return false, err
	}
	return oneStepFork, nil
}

func (vt *vertexTracker) prevVertex(ctx context.Context) (protocol.ChallengeVertex, error) {
	var prev protocol.ChallengeVertex
	if err := vt.cfg.chain.Call(func(tx protocol.ActiveTx) error {
		prevV, err := vt.vertex.Prev(ctx, tx)
		if err != nil {
			return err
		}
		if prevV.IsNone() {
			return errors.Wrapf(ErrPrevNone, "vertex with id: %#x", vt.vertex.Id())
		}
		prev = prevV.Unwrap()
		return nil
	}); err != nil {
		return nil, err
	}
	return prev, nil
}

// Merges to a vertex that already exists in the protocol by fetching its history commit
// from our state manager and then performing a merge transaction in the chain. Then,
// this method returns the vertex it merged to.
func (v *vertexTracker) mergeToExistingVertex(ctx context.Context) (protocol.ChallengeVertex, error) {
	var prev protocol.ChallengeVertex
	var mergingInto protocol.ChallengeVertex
	var parentCommit util.StateCommitment
	if err := v.cfg.chain.Call(func(tx protocol.ActiveTx) error {
		prevV, err := v.vertex.Prev(ctx, tx)
		if err != nil {
			return err
		}
		if prevV.IsNone() {
			return errors.New("no prev vertex found")
		}
		prev = prevV.Unwrap()
		parentStateCommitment, err := v.challenge.ParentStateCommitment(ctx, tx)
		if err != nil {
			return err
		}
		prevCommitment := prev.HistoryCommitment()
		commitment := v.vertex.HistoryCommitment()
		parentHeight := prevCommitment.Height
		toHeight := commitment.Height

		mergingToHistory, err := v.determineBisectionPointWithHistory(
			ctx,
			parentHeight,
			toHeight,
		)
		if err != nil {
			return err
		}
		manager, err := v.cfg.chain.CurrentChallengeManager(ctx, tx)
		if err != nil {
			return err
		}
		vertexId, err := manager.CalculateChallengeVertexId(ctx, tx, v.challenge.Id(), mergingToHistory)
		if err != nil {
			return err
		}
		vertex, err := manager.GetVertex(ctx, tx, vertexId)
		if err != nil {
			return err
		}
		if vertex.IsNone() {
			return errors.New("no vertex found to merge into")
		}
		mergingInto = vertex.Unwrap()
		parentCommit = parentStateCommitment
		return nil
	}); err != nil {
		return nil, err
	}
	mergingFrom := v.vertex
	mergedTo, err := v.merge(ctx, protocol.ChallengeHash(parentCommit.Hash()), mergingInto, mergingFrom)
	if err != nil {
		return nil, err
	}
	return mergedTo, nil
}

// TODO: Unused - need to refactor into something more manageable.
// TODO: Refactor as this function does too much. A vertex tracker should only be responsible
// for confirming its own vertex, not subchallenge vertices.
// nolint:unused
func (v *vertexTracker) confirmed(ctx context.Context) (bool, error) {
	// Can't confirm if the vertex is not in correct state.
	status := v.vertex.Status()
	if status != protocol.AssertionPending {
		return false, nil
	}

	var gotConfirmed bool

	if err := v.cfg.chain.Tx(func(tx protocol.ActiveTx) error {
		// Can't confirm if parent isn't confirmed, exit early.
		prev, err := v.vertex.Prev(ctx, tx)
		if err != nil {
			return err
		}
		if prev.IsNone() {
			return errors.New("no prev vertex")
		}
		prevStatus := prev.Unwrap().Status()
		// TODO: Vertex status different from assertion status.
		if prevStatus != protocol.AssertionConfirmed {
			return nil
		}

		// Can confirm if vertex's parent has a sub-challenge, and the sub-challenge has reported vertex as its winner.
		subChallenge, err := prev.Unwrap().GetSubChallenge(ctx, tx)
		if err != nil {
			return err
		}
		if !subChallenge.IsNone() {
			var subChallengeWinnerVertex util.Option[protocol.ChallengeVertex]
			subChallengeWinnerVertex, err = subChallenge.Unwrap().WinnerVertex(ctx, tx)
			if err != nil {
				return err
			}
			if !subChallengeWinnerVertex.IsNone() {
				winner := subChallengeWinnerVertex.Unwrap()
				if winner == v.vertex {
					if confirmErr := v.vertex.ConfirmForSubChallengeWin(ctx, tx); confirmErr != nil {
						return confirmErr
					}
					gotConfirmed = true
				}
				return nil
			}
		}

		// Can confirm if vertex's presumptive successor timer is greater than one challenge period.
		psTimer, err := v.vertex.PsTimer(ctx, tx)
		if err != nil {
			return err
		}
		if time.Duration(psTimer)*time.Second > v.cfg.challengePeriodLength {
			if confirmErr := v.vertex.ConfirmForPsTimer(ctx, tx); confirmErr != nil {
				return err
			}
			gotConfirmed = true
			return nil
		}

		// Can confirm if the challenge’s end time has been reached, and vertex is the presumptive successor of parent.
		if v.cfg.timeRef.Get().After(v.cfg.challengeCreationTime.Add(2 * v.cfg.challengePeriodLength)) {
			if confirmErr := v.vertex.ConfirmForChallengeDeadline(ctx, tx); confirmErr != nil {
				return err
			}
			gotConfirmed = true
		}
		return nil
	}); err != nil {
		return false, err
	}
	return gotConfirmed, nil
}
