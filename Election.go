// Copyright 2016 Comcast Cable Communications Management, LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// End Copyright

// Package leaderelection implements a leader election protocol in Go using the Zookeeper leader election recipe.
// The main steps to implement a leader election as as follows:
//
// Initialize an election
//
// Before leader election can take place the election needs to be initialized. This involves providing a live connection
// to Zookeeper and an existing Zookeeper node (resource) that represents the top level of the election to the NewElection function.
//
//	election, err := NewElection(zkConn, "/election")
//
// In the above example if zkConn is not valid or "/election" doesn't already exist an error will be
// returned.
//
// Run an election
//
// Running an election will register this election instance as interested in becoming leader for the election resource
// that was provided in the call to NewElection (e.g., "/election"). Multiple candidates may be registered for the same
// election resource and contend for leadership of the resource. One will be chosen as the leader and the rest will
// become followers. In the event that the leader exits prior to the election terminating one of the followers will
// become leader. Example:
//
//	go leaderElector.ElectLeader()
//
// This registers the candidate that created the election instance as a potential leader for the election resource. As
// it is started as a goroutine the candidate is expected to monitor one of several election related channels for
// election events.
//
// Events that can happen while an election is running
//
// There are several channels that must be monitored during an election. The first is the channel returned by
// election.Status(). This channel is used to indicate to a follower that it has become leader. It is also used to
// signal the end of the election. This signaling occurs when the Election instance closes the Status() channel. When
// election end has been signaled via the closing of the Status() channel the client is expected to stop using the
// election. Errors may also be returned via the Status() channel. Errors generally indicate there has
// been a problem with the election. A network partition from Zookeeper is an example of an error that may occur.
// Errors are unrecoverable and mean the election is over for that candidate. The connFailCh highlights that the
// client owns the Zookeeper connection and is responsible for handling any errors associated with the Zookeeper
// connection.
//
//	for {
//		select {
//		case status, ok = <- leaderElector.Status():
//			if !ok {
//			fmt.Println("\t\t\tChannel closed, election is terminated!!!")
//			leaderElector.Resign()
//			return
//			}
//			if status.Err != nil {
//				fmt.Println("Received election status error <<", status.Err, ">> for candidate <", leaderElector.candidateID, ">.")
//				leaderElector.Resign()
//				return
//			}
//			fmt.Println("Candidate received status message: <", status, ">.")
//			if status.Role == Leader {
//				doLeaderStuff(leaderElector, status, respCh, connFailCh, waitFor)
//				leaderElection.EndElection() // Terminates the election and signals all followers the election is over.
//				return
//			}
//		case <-connFailCh:
//			fmt.Println("\t\t\tZK connection failed for candidate <", status.CandidateID, ">, exiting")
//			respCh <- ElectionResponse{false, status.CandidateID}
//			leaderElector.Resign()
//			wg.Done()
//			return
//
//		case ... // Any other channels the client may need to monitor.
//		...
//		}
//	}
//
// The for-ever loop indicates that the election continues until one of the halting conditions described above occurs.
// The "case <- connFailCh:" branch is used to monitor for Zookeeper connection problems. It is up to the client to
// decide what to do in this event.
//
// On channel close or Error events the client is expected to resign from the election as shown in the above code
// snippet. When the leader is done with the work associated with the election it is expected to terminate the election
// by calling the EndElection method. This is required to properly clean up election resources including termination
// of any existing followers.
//
package leaderelection

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"sync"

	"time"

	"github.com/go-zookeeper/zk"
)

const (
	// Follower indicates that this candidate is following another candidate/leader.
	Follower = iota
	// Leader indicates that this candidate is the leader for the election.
	Leader
)

const electionOver = "DONE"

type (
	// Role represents whether the associated candidate is a Leader or Follower
	Role int

	// Election is a structure that represents a new instance of a Election. This instance can then
	// be used to request leadership for a specific resource.
	Election struct {
		status chan Status // Used to communicate state/ status changes to the client
		// E.g., Client is leader, election is over. It is closed
		// by Election goroutine (i.e., ElectLeader())
		resign chan struct{} // This is a signal channel used by the Client to indicate that
		// it is resigning from the election. It is closed by Client goroutine.
		end chan struct{} // This is a signal channel used by the Client to indicate that
		// the election is over and should be terminated. It is closed by Client goroutine
		zkEvents <-chan zk.Event // Used by Zookeeper to send watch events to interested clients.
		// It will be closed by Zookeeper
		triggerElection chan error // This is used by the ZK watch goroutine to signal that a watched
		// znode was deleted. This can mean that a followed candidate resigned or that the leader exited
		// unexpectedly. It will be closed by the Election goroutine (i.e., ElectLeader()).
		stopZKWatch chan struct{} // This is a signal channel used to indicate that a ZK watch
		// goroutine should exit.
		zkConn           *zk.Conn // This is the Zookeeper connection provided by the client.
		ElectionResource string   // This is the Zookeeper node that represents the overall election. Candidates
		clientName       string   // Client that created the election for instrumentation purposes.
		// will be children of this node.
		candidateID string // This is the ID assigned by Zookeeper that is associated with the ephemeral node
		// representing a candidate
		once sync.Once // Used to protect operations that should only be executed one time (i.e.,
		// closing a channel.
		doneChl <-chan zk.Event // A specific signal that the DONE node, which is created during Delete/EndElection,
		// was created. This event means that the election should be terminated for the receiving client.
	}

	// Status is used to communicate the current state of the Election.
	Status struct {
		CandidateID string // CandidateID is the identifier assigned to the associated Candidate for this Election.
		Err         error  // Err is used to communicate the specific error associated with this Election.
		// nil means there is no Err.
		NowFollowing string // NowFollowing is the CandidateID of the candidate that this Candidate is following.
		// If the followed Candidate is the Leader, then this Candidate will become Leader
		// if the current Leader fails. It will be an empty string ("") if the associated
		// candidate is the Leader.
		Role         Role   // Role is used to indicate if the associated Candidate is the Leader or a Follower.
		WasFollowing string // WasFollowing is the CandidateID of the Candidate that this Candidate was following
		// prior to the followed Candidate failing. It will be an empty string ("") if it wasn't previously
		// following any candidate.
	}
)

// String returns a formatted string representation of Status.
func (status Status) String() string {
	var statusErr string
	if status.Err == nil {
		statusErr = "<nil>"
	} else {
		statusErr = status.Err.Error()
	}
	return "Status:" +
		"    CandidateID:" + status.CandidateID +
		"    NowFollowing:" + status.NowFollowing +
		"    Role:" + strconv.Itoa(int(status.Role)) +
		"    WasFollowing:" + status.WasFollowing +
		"    Err:" + statusErr
}

// NewElection initializes a new instance of an Election that can later be used to request
// leadership for a specific resource.
//
// It accepts:
// zkConn - a connection to a running Zookeeper instance;
// electionResource - resource represents the thing  for which the election is being held.
// For example, /election/president for an election for president, /election/senator for
// an election for senator, etc.
//
// It will return either a non-nil Election instance and a nil error, or a nil
// Election and a non-nil error.
//
func NewElection(zkConn *zk.Conn, electionResource string, clientName string) (*Election, error) {
	if zkConn == nil {
		return nil, errors.New("zkConn must not be nil")
	}
	exists, _, err := zkConn.Exists(electionResource)
	if err != nil {
		return nil, fmt.Errorf("%s Error validating electionResource (%v). Error (%v) returned",
			"NewElection:", electionResource, err)
	} else if !exists {
		return nil, fmt.Errorf("%s Provided electionResource: <%q>: does not exist", "NewElection:", electionResource)
	}

	_, _, electionDoneChl, err := zkConn.ExistsW(strings.Join([]string{electionResource, electionOver}, "/"))
	if err != nil {
		return nil, fmt.Errorf("%s Error setting watch on DONE node", "NewElection:")
	}

	if clientName == "" {
		clientName = "name-not-provided"
	}

	return &Election{
		status: make(chan Status, 1),
		resign: make(chan struct{}),
		end:    make(chan struct{}),
		// zkEvents can't be created until an actual watch is placed on the associated zk node.
		// zkEvents: make(chan zk.Event),
		triggerElection: make(chan error, 1), // Only 1 message will be sent before the sending goroutine exits.
		// Buffering prevents blocking in the case where the client exits before a message is sent.
		stopZKWatch:      make(chan struct{}),
		zkConn:           zkConn,
		ElectionResource: electionResource,
		doneChl:          electionDoneChl,
		clientName:       clientName,
	}, nil
}

// ElectLeader will make the caller a candidate for leadership and determine if the
// candidate becomes the leader.
//
// ElectLeader returns:
// true if the candidate was elected leader, false otherwise. Candidates
// that aren't elected leader will be placed in the pool of possible leaders (aka followers).
// Candidates must explicitly resign if they don't want to be considered for future leadership.
func (le *Election) ElectLeader() {

	cleanupOnlyOnce := func() {
		cleanup(le)
	}

	// Run the initial election. Need to check for resignations or election termination that may have
	// happened between the time the election was requested and the return of runElection().
	// fmt.Printf("%s Starting new election for resource <%s>.\n", "ElectLeader:", le.ElectionResource)
	status := runElection(le)

	le.status <- status // le.status channel is buffered so this is guaranteed not to block
	// fmt.Printf("ElectLeader:"+"Sent client status message: <", status, ">.\n")
	for {
		select {
		case err := <-le.triggerElection:
			// ZK delete events will trigger a re-election if not previously cancelled (resign or end)
			if err != nil {
				status.Err = err
				// fmt.Printf("ElectLeader:"+" Error returned from ZK watch func(s), <", status, ">. \n"+
				//	"Sending Status to client")
			} else {
				determineLeader(le, &status)
			}

			// Send the updated status
			select {
			case le.status <- status:
				if err != nil {
					resignElection(le, cleanupOnlyOnce)
					return
				}
			case <-le.resign:
				resignElection(le, cleanupOnlyOnce)
				return
			case <-le.end:
				endElection(le, cleanupOnlyOnce)
				return
			}
		case <-le.resign:
			resignElection(le, cleanupOnlyOnce)
			return
		case <-le.end:
			endElection(le, cleanupOnlyOnce)
			return
		}
	}
}

// EndElection is called by the Client (leader) to signal that any work it was doing as a result of the
// election has completed.
//
// Ending an election results in all followers being notified that the election is over. Followers are expected to
// resign from the election and move on to whatever they do when not actively involved in an Election. Ending an
// election also results in the freeing of all resources associated with an election.
func (le *Election) EndElection() {
	close(le.end)
}

// Resign results in the resignation of the associated candidate from the election.
//
// Resign is called by the Client, either in a leader or follower role, to indicate
// that it is no longer interested in being a party to the election.
//
// If the candidate is the leader, then a new leader election will be triggered assuming
// there are other candidates.  If the candidate is not the leader then Resign merely
// results in the removal of the associated candidate from the set of possible leaders.
//
// Resign returns nil if everything worked as expected. It will return an error if there
// was any problem that prevented the complete resignation of the candidate. In the event
// that an error is returned the client will need to perform any processing appropriate to
// the failure.
func (le *Election) Resign() {
	// fmt.Printf("Resign: Candidate<%s>\n", le.candidateID)
	close(le.resign)
}

// Status returns a channel that is used by the library to provide election status updates to clients.
func (le *Election) Status() <-chan Status {
	return le.status
}

// String is the Stringer implementation for this type..
func (le *Election) String() string {
	return "Election:" +
		"\tCandidateID:" + le.candidateID +
		"\tResource:" + le.ElectionResource
}

// ----------------------------------------------------------------------------------------- //
// ------------------------------ [ Private functions] ------------------------------------- //
// ----------------------------------------------------------------------------------------- //

func resignElection(le *Election, cleanupOnlyOnce func()) {
	// Close channel before deleting candidate node. This is needed to prevent ZK watches from firing
	// for the watched candidate when it is deleted as part of resignation. It also ensures that the
	// associated goroutine exits.
	close(le.stopZKWatch)
	// -1 in the ZK call below means delete all versions of the node identified by candidateID. Only 1 version is expected.
	err := le.zkConn.Delete(le.candidateID, -1)
	if err != nil {
		// fmt.Println("resignElection:"+"Error deleting ZK node for candidate <", le.candidateID, "> during Resign. Error is ", err)
	}
	le.once.Do(cleanupOnlyOnce)
	return
}

func endElection(le *Election, cleanupOnlyOnce func()) {
	// Close channel before deleting candidate nodes. This is needed to prevent ZK watches from firing
	// when candidates are deleted as part of completing/ending the election. It also ensures that the
	// associated goroutine exits.
	close(le.stopZKWatch)
	// All candidates need to be deleted so no one can assume leadership of a terminated election.
	deleteAllCandidates(le)
	le.once.Do(cleanupOnlyOnce)
	return
}

func runElection(le *Election) Status {
	status := Status{}
	zkWatchChl := makeOffer(le, &status)
	if status.Err != nil {
		status.Err = fmt.Errorf("%s Unexpected error attempting to nominate candidate. Error <%v>",
			"runElection:", status.Err)
		return status
	}
	le.zkEvents = zkWatchChl
	le.candidateID = status.CandidateID

	// fmt.Printf("runElection: "+"ElectLeader: le.candidate.CandidateID:\n", le.candidateID)
	determineLeader(le, &status)
	if status.Err != nil {
		status.Err = fmt.Errorf("%s Unexpected error attempting to determine leader. Error (%v)",
			"runElection:", status.Err)
		return status
	}
	// fmt.Println("runElection: "+"Election Result: Leader?", status.Role, "; Candidate info:", le.candidateID)

	return status
}

func makeOffer(le *Election, status *Status) <-chan zk.Event {
	flags := int32(zk.FlagSequence | zk.FlagEphemeral)
	acl := zk.WorldACL(zk.PermAll)

	candidates, err := getCandidates(le, status)
	if err != nil {
		status.Err = fmt.Errorf("%s Received error (%v) attempting to retrieve candidates",
			"makeOffer:", err)
		return nil
	}

	if isElectionOver(candidates) {
		status.Err = fmt.Errorf("%s %s", "makeOffer:", ElectionCompletedNotify)
		return nil
	}

	// Make offer
	// TODO: Perhaps this should be changed from le.zkConn.Create(...) to le.zkConn.CreateProtectedEphemeralSequential(...)?
	// TODO: Needs more research. See the go-zookeeper code at https://github.com/go-zookeeper/tree/master/zk
	// TODO: for details.
	cndtID, err := le.zkConn.Create(strings.Join([]string{le.ElectionResource, "le_"}, "/"), []byte(le.clientName), flags, acl)
	if err != nil {
		status.Err = fmt.Errorf("%s Error <%v> creating candidate node for election <%v>",
			"makeOffer:", err, le.ElectionResource)
		return nil
	}

	exists, _, watchChl, err := le.zkConn.ExistsW(cndtID)
	if err != nil {
		status.Err = fmt.Errorf("%s Received error (%v) attempting to watch newly created node (%v)",
			"makeOffer:", err, cndtID)
		return nil
	}
	if !exists {
		// It's possible during EndElection or DeleteElection for a candidate's ZK  node to be deleted between the
		// time it was created and when this existence check is performed. So return an error (really a status) that
		// the election is being ended or deleted.
		status.Err = fmt.Errorf("%s Newly created node (%v) doesn't exist - %s", "makeOffer:", cndtID,
			ElectionCompletedNotify)
		return nil
	}

	// fmt.Printf("makeOffer: le.candidate.CandidateID: %v \n", cndtID)
	status.CandidateID = cndtID
	return watchChl
}

// determineLeader() modifies the status parameter. One important field that is updated is the
// Err field. It is important to check this after the function is called.
func determineLeader(le *Election, status *Status) {
	candidates, err := getCandidates(le, status)
	if err != nil {
		status.Err = fmt.Errorf("%s %v", "determineLeader:", err)
		return
	}
	if len(candidates) == 0 {
		// fmt.Printf("determineLeader: "+"No children exist in ZK, not even me:%s\n", le.candidateID)
		status.Err = fmt.Errorf("%s No Leader candidates exist in ZK. Candidate requesting children is (%v)",
			"determineLeader:", status.CandidateID)
		return
	}

	if isElectionOver(candidates) {
		status.Err = fmt.Errorf("%s %s", "determineLeader:", ElectionCompletedNotify)
		return
	}

	iAmLeader, shortCndtID := amILeader(status, candidates, le)
	if iAmLeader {
		return
	}

	followAnotherCandidate(le, shortCndtID, status, candidates)
	return
}

func getCandidates(le *Election, status *Status) ([]string, error) {
	candidates, _, err := le.zkConn.Children(le.ElectionResource)
	if err != nil {
		status.Err = fmt.Errorf("%s Received error getting candidates from ZK. Error is <%v>. "+
			"Candidate requesting children is <%v>.", "getCandidates:", err, status.CandidateID)
		return nil, status.Err
	}
	return candidates, status.Err
}

func amILeader(status *Status, candidates []string, le *Election) (bool, string) {
	// Compare the ID of the candidate associated with this Election instance to the
	// child with the lowest ID. If it matches then it's the leader.
	pathNodes := strings.Split(status.CandidateID, "/")
	lenPath := len(pathNodes)
	shortCndtID := pathNodes[lenPath-1]
	// fmt.Printf("amILeader: Election ID: %s\n", shortCndtID)

	sort.Strings(candidates)
	if strings.EqualFold(shortCndtID, candidates[0]) {
		status.Role = Leader
		status.WasFollowing = status.NowFollowing
		status.NowFollowing = ""

		// Detect if the leader node is unexpectedly deleted
		go watchForLeaderDeleteEvents(le.zkEvents, le.triggerElection, le.stopZKWatch, le.candidateID,
			le.doneChl)
	}
	return status.Role == Leader, shortCndtID
}

// I'm not leader, so watch next highest candidate
func followAnotherCandidate(le *Election, shortCndtID string, status *Status, candidates []string) {
	watchedNode, err := findWhoToFollow(candidates, shortCndtID, le)
	if err != nil && strings.Contains(err.Error(), leaderWhileDeterminingWhoToFollow) {
		// I now find myself in the leader position. So leader election has to be rerun.
		le.triggerElection <- nil //
		return
	}
	if err != nil {
		status.Err = fmt.Errorf("%s %v", "followAnotherCandidate:", err)
		return
	}

	exists, _, zkFollowingWatchChl, err := le.zkConn.ExistsW(watchedNode)
	if err != nil {
		status.Err = fmt.Errorf("%s ZK error checking existence of %q. Error is (%v)",
			"followAnotherCnadidate:", watchedNode, err)
		return
	}
	if !exists {
		// fmt.Printf("followAnotherCandidate: Watched candidate %s doesn't exist in ZK\n", watchedNode)

		// It's possible that the watched candidate was deleted just prior to the existence test, e.g., via Resign().
		// It's also possible that the election is in the process of being deleted and this is what resulted in the
		// watched candidate being deleted. Rerunning the election will detect if the election is over and the situation
		// where the watched candidate is deleted. In either case it doesn't the right thing.
		le.triggerElection <- nil
	}

	// fmt.Printf("followAnotherCandidate: Candidate: %s is watching candidate: %s\n", le.candidateID, watchedNode)
	// fmt.Printf("followAnotherCandidate: Candidate %s will watch on channel %v\n", le.candidateID, zkFollowingWatchChl)

	status.WasFollowing = status.NowFollowing
	status.NowFollowing = watchedNode
	status.Role = Follower

	go watchForFollowerEvents(zkFollowingWatchChl, le.zkEvents, le.triggerElection, le.stopZKWatch, le.candidateID,
		le.doneChl)
}

func watchForLeaderDeleteEvents(ldrDltWatchChl <-chan zk.Event, notifyChl chan<- error,
	stopZKWatch <-chan struct{}, candidateID string, electionCompleted <-chan zk.Event) {
	// Continue until a zk.EventNodeDeleted message is received or a message is received on the stopZKWatch channel.
	for {
		select {
		case doneWatchEvent := <-electionCompleted:
			// fmt.Printf("watchForLeaderDeleteEvents: Candidate %s received notification that the election is over. Watch event is %v\n",
			// candidateID, doneWatchEvent)
			notifyChl <- fmt.Errorf("%s - %s - Election 'DONE' node created or deleted <%v>, the election is over"+
				" for candidate <%s>.", "watchForLeaderDeleteEvents", ElectionCompletedNotify, doneWatchEvent, candidateID)
			return

		// My node was deleted. Need to relay message back to Election goroutine that it needs to
		// stop working and exit.
		case delWatchEvent := <-ldrDltWatchChl:
			if delWatchEvent.Type == zk.EventNodeDeleted {
				// fmt.Println("watchForLeaderDeleteEvents: Leader ", candidateID, " was deleted, notification message is <",
				//	delWatchEvent, ">")
				err := fmt.Errorf("%s Leader (%v) has been deleted", "watchForLeaderDeleteEvents", candidateID)
				notifyChl <- err
				// fmt.Println("watchForLeaderDeleteEvents: Done with Candidate deletion for", candidateID)
				return
			}
			return

		// Election goroutine is signaling this goroutine to exit.
		case <-stopZKWatch:
			// fmt.Println("watchForLeaderDeleteEvents: watchForLeaderDeleteEvents: stopZKWatch msg received.")
			return
		}
	}
}

func watchForFollowerEvents(followingWatchChnl <-chan zk.Event, selfDltWatchChl <-chan zk.Event, notifyChl chan<- error,
	stopZKWatch <-chan struct{}, candidateID string, electionCompleted <-chan zk.Event) {
	// Continue until a zk.EventNodeDeleted message is received or a message is received on the stopZKWatch channel.
	for {
		select {
		case doneWatchEvent := <-electionCompleted:
			// fmt.Println("watchForFollowerEvents: Candidate ", candidateID, " received notification that the election"+
			//	" is over. Watch event <", doneWatchEvent, ">")
			notifyChl <- fmt.Errorf("%s - %s - Election 'DONE' node created or deleted <%v>, the election is over"+
				" for candidate <%s>.", "watchForFollowerEvents", ElectionCompletedNotify, doneWatchEvent, candidateID)
			return

		// The node I'm following was deleted. I need to go through leader election again to find out
		// if I'm leader or who I'm otherwise following.
		case watchEvt := <-followingWatchChnl:
			if watchEvt.Type == zk.EventNodeDeleted {
				// fmt.Println("watchForFollowerEvents: Candidate ", candidateID, " received a watch event, starting leader "+
				//	"re-election process. Watch event is <", watchEvt, ">")
				notifyChl <- nil
				return
			}
		// My node was deleted. Need to relay message back to Election client that this Election is
		// no longer valid
		case delWatchEvent := <-selfDltWatchChl:
			if delWatchEvent.Type == zk.EventNodeDeleted {
				err := fmt.Errorf("%s - %s - Candidate (%s) (i.e., me) has been deleted", "watchForFollowerEvents",
					ElectionSelfDltNotify, candidateID)
				// fmt.Println("watchForFollowerEvents: Candidate ", candidateID, " was deleted, notification message is <",
				//	delWatchEvent, ">. Returning error <", err, ">.")
				notifyChl <- err
				// fmt.Println("watchForFollowerEvents: Done with Candidate deletion for ", candidateID)
				return
			}
		// Election goroutine is signaling this goroutine to exit.
		case <-stopZKWatch:
			// fmt.Println("watchForFollowerEvents: watchForFollowerEvents: stopZKWatch msg received.")
			return
		}
	}
}

// Due to race conditions during ending or deleting an election, a candidate may find that it has become leader
// between the time it was determined that it wasn't the leader and now, when it's trying to find who to follow.
// This same race may also result in the candidate being deleted just prior to this function being called. Both cases
// are acceptable. The latter means the election is ending.
func findWhoToFollow(candidates []string, shortCndtID string, le *Election) (string, error) {
	idx := sort.SearchStrings(candidates, shortCndtID)
	if idx == 0 {
		return "", fmt.Errorf("%s %s Candidate is <%s>", "findWhoToFollow", leaderWhileDeterminingWhoToFollow, shortCndtID)
	}
	if idx == len(candidates) {
		return "", fmt.Errorf("%s Error finding candidate in child list. Candidate attempting match is (%s) - %v",
			"findWhoToFollow", shortCndtID, ElectionCompletedNotify)
	}

	// fmt.Println("findWhoToFollow: Candidate <", shortCndtID, "> found (or not) at index <", idx, ">.")
	// fmt.Println("findWhoToFollow: My ID is <", shortCndtID, ">. Expecting same ID in child list at position <",
	//	idx, ">. Child ID <", candidates[idx], "> was found at that position.")

	if !strings.EqualFold(candidates[idx], shortCndtID) {
		return "", fmt.Errorf("%s Error finding candidate in child list. Candidate attempting match is (%v). "+
			"Candidate located at position <%d> is <%s> - %v",
			"findWhoToFollow", shortCndtID, idx, candidates[idx], ElectionCompletedNotify)
	}

	nodeToWatchIdx := idx - 1

	watchedNode := strings.Join([]string{le.ElectionResource, candidates[nodeToWatchIdx]}, "/")
	return watchedNode, nil
}

func cleanup(le *Election) {
	close(le.status)
}

func deleteAllCandidates(le *Election) {
	doneNodePath := strings.Join([]string{le.ElectionResource, electionOver}, "/")
	// TODO: Return error?
	DeleteCandidates(le.zkConn, le.ElectionResource, doneNodePath)
}

// DeleteElection removes the election resource passed to NewElection.
func DeleteElection(zkConn *zk.Conn, electionResource string) error {
	// fmt.Printf("%s: Entered. election resource = <%s>\n", "DeleteElection", electionResource)
	doneNodePath, err := addDoneNode(zkConn, electionResource)
	if err != nil {
		return fmt.Errorf("%s Error adding 'DONE' node: <%v>", "DeleteElection", err)
	}

	err = DeleteCandidates(zkConn, electionResource, doneNodePath)
	if err != nil {
		return fmt.Errorf("%s Error deleting candidates: <%v>", "DeleteElection", err)
	}

	// fmt.Println("DeleteElection: Deleting root Job node <", electionResource,
	//	">, including the 'Done' node <", doneNodePath, ">.")

	err = deleteDoneNodeAndElection(zkConn, electionResource, doneNodePath)
	// Missing node and ZK API errors are OK
	if err != nil && err != zk.ErrNoNode {
		exists, _, err2 := zkConn.Exists(electionResource)
		if err2 == nil && !exists {
			// fmt.Println("DeleteElection: Despite an error, the Election node was deleted (or otherwise doesn't exist."+
			//	"The actual error received is ", err)
		} else {
			return fmt.Errorf("%s Unexpected error received from leaderelection.DeleteElection: %v", "DeleteElection", err)
		}
	} else if err != nil {
		// fmt.Println("DeleteElection: An expected error, ('node does not exist'), received "+
		//	"from leaderelection.DeleteElection: <%v>", err)
	}

	return nil
}

// DeleteCandidates will remove all the candidates for the provided electionName.
func DeleteCandidates(zkConn *zk.Conn, electionName string, doneNodePath string) error {
	// Retrying once is sufficient to ensure that any candidates added after the "DONE" node was added
	// will be deleted. Retrying fixes a race between adding a new candidate and adding the "DONE" node.
	retries := 1
	for i := 0; i < retries; i++ {
		jobWorkers, _, err := zkConn.Children(electionName)
		// zk.ErrNoNode means that no candidates exist for an election. This is because some other process previously
		// deleted the candidates or the candidates never existed. Since candidate deletion (other than during resignation)
		// is a "best effort" operation, this is OK.
		if err != nil && err != zk.ErrNoNode {
			return fmt.Errorf("%s: Received error getting candidates from ZK. Error is <%v>. \n", "DeleteCandidates", err)
		}

		err = runDeleteCandidates(zkConn, electionName, jobWorkers, doneNodePath)
		if err != nil {
			return fmt.Errorf("%s: Received error deleting candidates. Error is <%v>. \n", "DeleteCandidates", err)
		}
	}

	return nil
}

func runDeleteCandidates(zkConn *zk.Conn, electionName string, candidates []string, doneNodePath string) error {
	numCndts := len(candidates)
	if numCndts <= 0 {
		// fmt.Printf("%s: ZK returned <%d> children, nothing to delete\n", "runDeleteCandidates", numCndts)
		return nil
	}

	// Candidates must be deleted in reverse order so watches don't get triggered. Otherwise undesired leader
	// elections would take place. Hence starting the for-loop at numWorkers-1 and going backwards.
	sort.Sort(sort.Reverse(sort.StringSlice(candidates)))
	var deleteWorkerRqsts []interface{}
	for _, shortCndt := range candidates {
		longCndt := strings.Join([]string{electionName, shortCndt}, "/")
		if strings.Contains(longCndt, doneNodePath) {
			continue
		}
		deleteWorkerOp := zk.DeleteRequest{Path: longCndt}
		deleteWorkerRqsts = append(deleteWorkerRqsts, &deleteWorkerOp)
	}

	// fmt.Printf("%s Deleting <%d> workers for job <%s>\n", "runDeleteCandidates", len(deleteWorkerRqsts), electionName)
	var err error
	// limit total retries to 1 second elapsed time. This is a hard limit.
	// Before adding that loop, for unknown reasons, ZK could return no children for a preceeding GetChildren() call,
	// but return an error when attempting to delete the parent node. This behavior was more pronounced as the number
	// of children to be deleted grows. Fix at the time was adding 1 sec sleep which was allowing the parent node
	// to be reliably deleted after DeleteCandidates() is called above.
	for i := 0; i < 10; i++ {
		_, err = zkConn.Multi(deleteWorkerRqsts...)
		if err == nil {
			// life is good, the workers were successfully deleted
			break
		}
		if err != nil {
			jobWorkers, _, zkErr := zkConn.Children(electionName)
			if zkErr != nil {
				// there was a real problem, not all children were deleted
				continue
			}
			if len(jobWorkers) == 1 { // There should be 1 child, the DONE node
				// life is still good, all children were successfully deleted
				break
			}
		}
		// fmt.Printf("%s Error <%v> deleting %d workers for job %s\n", "runDeleteCandidates", err,
		//	len(deleteWorkerRqsts), electionName)
		time.Sleep(10 * time.Millisecond)
	}
	// Even if there is an error deleting the existing set of candidates we'll continue with
	// election deletion. If this really is an error, it'll get caught in the next step in
	// delete election.
	return nil
}

func addDoneNode(zkConn *zk.Conn, electionName string) (string, error) {
	var flags int32
	acl := zk.WorldACL(zk.PermAll)
	nodePath, err := zkConn.Create(strings.Join([]string{electionName, electionOver}, "/"), []byte("done"),
		flags, acl)
	// zk.ErrNoNode means that the election node doesn't exist. This is because some other process previously
	// deleted it or it never existed. Since election deletion is a "best effort" operation, this is OK.
	// zk.ErrNodeExists means that the DONE node exists and that another DeleteElection is in progress. This
	// is also OK.
	if err != nil && err != zk.ErrNoNode && err != zk.ErrNodeExists {
		return "", fmt.Errorf("%s Error adding 'ElectionOver` (aka done) node. Error: <%v>", "addDoneNode", err)
	}
	// If electionOver node exists, the zkConn.Create(...) above will return "" for nodePath. So it has
	// to be constructed from the parts.
	if err != nil && err == zk.ErrNodeExists {
		nodePath = strings.Join([]string{electionName, electionOver}, "/")
	}

	return nodePath, nil
}

func deleteDoneNodeAndElection(zkConn *zk.Conn, electionName, doneNodePath string) error {
	deleteDoneNode := zk.DeleteRequest{Path: doneNodePath, Version: -1}
	deleteElectionNode := zk.DeleteRequest{Path: electionName, Version: -1}
	deleteRequests := []interface{}{&deleteDoneNode, &deleteElectionNode}

	var err error
	// limit total retries to 1 second elapsed time. This is a hard limit.
	// Before adding that loop, for unknown reasons, ZK could return no children for a preceeding GetChildren() call,
	// but return an error when attempting to delete the parent node. This behavior was more pronounced as the number
	// of children to be deleted grows. Fix at the time was adding 1 sec sleep which was allowing the parent node
	// to be reliably deleted after DeleteCandidates() is called above.
	for i := 0; i < 10; i++ {
		_, err = zkConn.Multi(deleteRequests...)
		if err == nil {
			// life is good, the deletions worked
			break
		}
		exists, _, err2 := zkConn.Exists(electionName)
		if err2 == nil && !exists {
			// life is good, the election was actually deleted
			break
		}
		if err2 == zk.ErrNoNode {
			// life is still good, the election was actually deleted in a prior iteration
			break
		}
		// fmt.Printf("%s Error <%v> deleting ZK 'done' <%s> and election <%s> nodes\n",
		//	"deleteDoneNodeAndElection", err, doneNodePath, electionName)
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("%s Error <%v> deleting ZK 'done' <%s> and election <%s> nodes",
			"deleteDoneNodeAndElection", err, doneNodePath, electionName)
	}

	return nil
}

func isElectionOver(candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(candidate, electionOver) {
			return true
		}
	}
	return false
}
