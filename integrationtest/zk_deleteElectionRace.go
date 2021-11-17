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

package integrationtest

import (
	"fmt"
	"log"
	"os"
	"time"

	"sync"

	"strings"

	"github.com/Comcast/go-leaderelection"
	"github.com/go-zookeeper/zk"
)

const numCandidates = 500

type zkDeleteElectionRaceTest struct {
	testSetup  ZKTestSetup
	candidates []*leaderelection.Election
	zkConn     *zk.Conn
	done       chan struct{}
	errChl     chan error
	wg         sync.WaitGroup
	info       *log.Logger
	error      *log.Logger
}

func (test *zkDeleteElectionRaceTest) CreateTestData() (err error) {
	test.info.Println("CreateTestData()")
	return nil
}

func (test *zkDeleteElectionRaceTest) InitFunc() ([]time.Duration, error) {
	test.info.Println("InitFunc()")

	zkConn, _, err := zk.Connect([]string{test.testSetup.zkURL}, test.testSetup.heartBeat)
	if err != nil {
		test.error.Printf(" %s Error in zk.Connect (%s): %v", "zkDeleteElectionRaceTest", test.testSetup.zkURL, err)
		return nil, err
	}

	test.zkConn = zkConn
	test.done = make(chan struct{})
	test.errChl = make(chan error, 10000) // Never block!

	// Create the election node in ZooKeeper
	_, err = test.zkConn.Create(test.testSetup.electionNode, []byte("data"), 0, zk.WorldACL(zk.PermAll))
	if err != nil {
		return nil, fmt.Errorf("%s Error creating the election node (%s): %v", "zkDeleteElectionRaceTest",
			test.testSetup.electionNode, err)
	}

	return []time.Duration{
		time.Millisecond * 10,
		time.Millisecond * 1000, // Allow time for goroutines to spin up
		time.Millisecond * 1,
	}, nil
}

func (test *zkDeleteElectionRaceTest) StepFunc(idx int) error {
	switch idx {
	case 0:
		test.info.Printf("Step %d: Create Election.", idx)
		createAndRunCandidates(test)
		time.Sleep(500 * time.Millisecond) // Let candidates spin up before waiting to avoid race on test.wg
		waitForCandidatesToComplete(test)

	case 1:
		test.info.Printf("Step %d: Delete Election", idx)
		createAndRunCandidates(test) // Create more candidates to stimulate the race condition
		// The election delete should work with only 1 retry given that the "done" node will prevent
		// new consumers from being added after the "done" node is created.
		err := leaderelection.DeleteElection(test.zkConn, test.testSetup.electionNode)
		if err != nil {
			return fmt.Errorf("%s Error deleting election! Error: <%v>, Election Node: <%v>",
				"zkDeleteElectionRaceTest", err, test.testSetup.electionNode)
		}

	case 2:
		test.info.Printf("Step %d: Wait for candidates to exit...", idx)

		// TODO: It looks like the candidates are being deleted in the wrong order - i.e., not highest to lowest? Why?
		// TODO: Is it a problem?
		for {
			select {
			case err := <-test.errChl:
				if strings.Contains(err.Error(), leaderelection.ElectionCompletedNotify) ||
					strings.Contains(err.Error(), leaderelection.ElectionSelfDltNotify) ||
					strings.Contains(err.Error(), zk.ErrNoNode.Error()) {
					continue // still need to wait for all goroutines to complete (i.e., test.done below)
				}
				panicMsg := fmt.Sprintf("%s Unexpected error received while waiting for Election deletion <%v>", "zkDeleteElectionRaceTest", err)
				panic(panicMsg)
				return fmt.Errorf("%s Unexpected error received while waiting for Election deletion: <%v>",
					"zkDeleteElectionRaceTest", err)
			case <-test.done:
				close(test.errChl)
				return nil
			case <-time.After(12 * time.Second):
				panic("SHOW ME WHY I'M BLOCKED")
			}
		}
	}
	return nil
}

func (test *zkDeleteElectionRaceTest) EndFunc() error {
	test.info.Println("EndFunc(): Verify election was successfully deleted.")

	defer test.zkConn.Close()

	exists, _, err := test.zkConn.Exists(test.testSetup.electionNode)
	if err != nil {
		return fmt.Errorf("%s Error checking if election node exists! "+
			"Error: <%v>, Election Node: <%v>", "zkDeleteElectionRaceTest", err, test.testSetup.electionNode)
	}

	if exists {
		return fmt.Errorf("%s Election node <%v> exists.",
			"zkDeleteElectionRaceTest", test.testSetup.electionNode)
	}

	// SUCCESS
	return nil
}

// NewZKDeleteElectionRaceTest creates a new happy path test case
func NewZKDeleteElectionRaceTest(setup ZKTestSetup) *ZKTest {
	deleteElectionRacetest := zkDeleteElectionRaceTest{
		testSetup:  setup,
		candidates: make([]*leaderelection.Election, 0),
		info:       log.New(os.Stderr, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile),
		error:      log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile),
	}
	zkDeleteRaceTest := ZKTest{
		Desc:      "ZK Election Delete Race Test",
		Callbacks: &deleteElectionRacetest,
		Err:       nil,
	}

	return &zkDeleteRaceTest
}

func runCandidate(errorLogger *log.Logger, zkConn *zk.Conn, electionPath string, wg *sync.WaitGroup, errChl chan<- error) {

	leaderElector, err := leaderelection.NewElection(zkConn, electionPath, "myHostName")
	if err != nil {
		wg.Done()
		errChl <- fmt.Errorf("%s Error creating a new election, abandoning. Error: <%v>", "zkDeleteElectionRaceTest", err)
		return
	}

	go leaderElector.ElectLeader()

	var status leaderelection.Status
	var ok bool

	for {
		select {
		case status, ok = <-leaderElector.Status():
			if !ok {
				leaderElector.Resign()
				wg.Done()
				return
			}

			if status.Err != nil {
				if !strings.Contains(status.Err.Error(), leaderelection.ElectionCompletedNotify) {
					errChl <- fmt.Errorf("Received election status error <%v> for candidate <%s>.", status.Err, status.CandidateID)
				}
				leaderElector.Resign()
				wg.Done()
				return
			}

			if status.Role == leaderelection.Leader {
				// Do some work, in this test the leader should still be doing work when the election is deleted.
				// Otherwise it's an error.
				select {
				case status, ok = <-leaderElector.Status():
					leaderElector.Resign()
					wg.Done()
					return
				case <-time.After(time.Millisecond * 10000): // Wait long enough to give the test a good chance of deleting the election
					errorLogger.Println("Candidate <", status.CandidateID, "> completed it's work before the election was deleted")
					// There will be one more status message coming from the leaderElector. This is the status
					// message associated with the client deletion notification which happens when an election
					// is deleted.
					// TODO: deleted to avoid blocking receive after added DONE channel watch in watchForFollower
					<-leaderElector.Status()
					errChl <- fmt.Errorf("Candidate <%s> completed it's work before the election was deleted", status.CandidateID)
					leaderElector.Resign()
					wg.Done()
					return
				}
			}
		case <-time.After(time.Second * 30):
			errorLogger.Println("ERROR!!! Timer expired, stop waiting to become leader for", status.CandidateID)
			leaderElector.Resign()
			wg.Done()
			return
		}
	}
}

func createAndRunCandidates(test *zkDeleteElectionRaceTest) {
	for i := 0; i < numCandidates; i++ {
		test.wg.Add(1)
		go runCandidate(test.error, test.zkConn, test.testSetup.electionNode, &test.wg, test.errChl)
	}
}

func waitForCandidatesToComplete(test *zkDeleteElectionRaceTest) {
	go func() {
		test.wg.Wait()
		close(test.done)
	}()
}
