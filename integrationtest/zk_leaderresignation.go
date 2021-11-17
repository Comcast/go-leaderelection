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
	"sync"
	"time"

	"github.com/Comcast/go-leaderelection"
	"github.com/go-zookeeper/zk"
)

// zkLeaderResignationTest is the struct that controls the test.
type zkLeaderResignationTest struct {
	testSetup        ZKTestSetup
	leaderElecInst   *leaderelection.Election
	followerElecInst *leaderelection.Election
	follower2Elector *leaderelection.Election
	wg               *sync.WaitGroup
	zkConn           *zk.Conn
	info             *log.Logger
	error            *log.Logger
}

// CreateTestData: Implement the zkTestCallbacks interface
func (test *zkLeaderResignationTest) CreateTestData() (err error) {
	test.info.Println("CreateTestData()")
	return nil
}

// CreateTestData: Implement the zkTestCallbacks interface
// Initialize the steps
func (test *zkLeaderResignationTest) InitFunc() ([]time.Duration, error) {
	test.info.Println("InitFunc()")

	// Create the connection to Zookeeper
	zkConn, _, err := zk.Connect([]string{test.testSetup.zkURL}, test.testSetup.heartBeat)
	test.zkConn = zkConn
	test.wg = &sync.WaitGroup{}

	if err != nil {
		test.error.Printf("leaderResignationTest: Error in zk.Connect (%s): %v",
			test.testSetup.zkURL, err)
		return nil, err
	}

	// Create the election node in ZooKeeper
	_, err = test.zkConn.Create(test.testSetup.electionNode,
		[]byte("data"), 0, zk.WorldACL(zk.PermAll))
	if err != nil {
		test.error.Printf("leaderResignationTest: Error creating the election node (%s): %v",
			test.testSetup.electionNode, err)
		return nil, err
	}

	return []time.Duration{
		time.Second * 1,
		time.Second * 1,
		time.Second * 1,
		time.Second * 1,
		time.Second * 1,
	}, nil
}

// CreateTestData: Implement the zkTestCallbacks interface
func (test *zkLeaderResignationTest) StepFunc(idx int) error {
	switch idx {
	case 0: // Create the Election
		test.info.Printf("Step %d: Create Election.", idx)

		elector, err := leaderelection.NewElection(test.zkConn, test.testSetup.electionNode, "myhostname")
		test.leaderElecInst = elector

		if err != nil {
			test.error.Printf("ldrResignationTest: Error in NewElection (leader) (%s): %v",
				test.testSetup.electionNode, err)
			return err
		}

		test.followerElecInst, err = leaderelection.NewElection(test.zkConn,
			test.testSetup.electionNode, "myhostname")

		if err != nil {
			test.error.Printf("ldrResignationTest: Error in NewElection (follower) (%s): %v",
				test.testSetup.electionNode, err)
			return err
		}

	case 1: // Elect the leader. Add a follower
		test.info.Printf("Step %d.a: Elect Leader", idx)
		test.wg.Add(1)
		go func() {
			test.leaderElecInst.ElectLeader()
			test.wg.Done()
		}()
		status := <-test.leaderElecInst.Status()

		// We expect this to succeed since no one is competing for this election
		// excpet this test.
		if status.Role != leaderelection.Leader {
			// Print the list of candidates attached to the election node so we know
			// why we failed.
			children, stat, _ := test.zkConn.Children(test.testSetup.electionNode)
			test.info.Printf("List of children: %v, stat(%s): %v\n", children,
				test.testSetup.electionNode, stat)
			return fmt.Errorf("ldrResignationTest: Expected to be a leader but am not")
		}

		test.info.Printf("zkLeaderResignationtest: Candidate %s became the leader for Election: %v",
			status.CandidateID,
			test.leaderElecInst)

		test.info.Printf("Step %d.b: Add a follower", idx)
		// Make another candidate sign up for the same election
		test.wg.Add(1)
		go func() {
			test.followerElecInst.ElectLeader()
			test.wg.Done()
		}()

		fStatus := <-test.followerElecInst.Status()
		// We expect this client to NOT become the leader since we already have a leader
		if fStatus.Role != leaderelection.Follower {
			// Print the list of candidates attached to the election node so we know
			// why we failed.
			children, stat, _ := test.zkConn.Children(test.testSetup.electionNode)
			test.info.Printf("List of children: %v, stat(%s): %v\n", children,
				test.testSetup.electionNode, stat)
			return fmt.Errorf("ldrResignationTest: Did not expect to be the leader but I am")
		}

		test.info.Printf("zkLeaderResignationtest: Candidate %s became the follower for Election: %v",
			fStatus.CandidateID,
			test.followerElecInst)

		// Print the list of candidates attached to the election node now.
		children, stat, _ := test.zkConn.Children(test.testSetup.electionNode)
		test.info.Printf("List of children: %v, stat(%s): %v\n", children, test.testSetup.electionNode, stat)

	case 2: // Have the leader resign
		test.info.Printf("Step %d: Leader Resign", idx)
		test.leaderElecInst.Resign()
		select {
		case lStatus, ok := <-test.leaderElecInst.Status():
			if ok {
				return fmt.Errorf("Leader returned status %s after resign was called", lStatus)
			}
		case <-time.After(1 * time.Second):
			return fmt.Errorf("Leader failed to close status channel within 1 second of resign")
		}

	case 3: // Check if the follower is the new leader
		test.info.Printf("Step %d: Check for new leader", idx)
		// Print the list of candidates attached to the election node now.
		children, stat, _ := test.zkConn.Children(test.testSetup.electionNode)
		test.info.Printf("List of children: %v, stat(%s): %v\n", children, test.testSetup.electionNode, stat)

		select {
		case fStatus := <-test.followerElecInst.Status():
			if fStatus.Role != leaderelection.Leader {
				return fmt.Errorf("Follower did not become leader as expected")
			}
			if fStatus.NowFollowing != "" {
				return fmt.Errorf("Follower now following did not become nil when it become leader")
			}
		case <-time.After(1 * time.Second):
			return fmt.Errorf("ldrResignationTest: Didn't receive ldrship change notification in time")
		}

	case 4: // Check if when leader (or some other client) comes back, he is simply a follower
		var err error
		test.info.Printf("Step %d: Test new follower joining", idx)
		test.follower2Elector, err = leaderelection.NewElection(test.zkConn, test.testSetup.electionNode, "myHostName")
		if err != nil {
			test.error.Printf("ldrResignationTest: Error in NewElection (leader) (%s): %v",
				test.testSetup.electionNode, err)
			return err
		}

		test.wg.Add(1)
		go func() {
			test.follower2Elector.ElectLeader()
			test.wg.Done()
		}()
		f2Status := <-test.follower2Elector.Status()
		if f2Status.Role != leaderelection.Follower {
			return fmt.Errorf("ldrResignationTest: follower 2 became a leader")
		}
		test.info.Printf("Test Success")
	}
	return nil
}

// CreateTestData: Implement the zkTestCallbacks interface
// Verify the results
func (test *zkLeaderResignationTest) EndFunc() error {
	test.info.Println("EndFunc(): Verification")

	// Have the any child nodes resign so that we can delete the election node
	test.follower2Elector.Resign()
	test.followerElecInst.Resign()
	test.wg.Wait()

	err := test.zkConn.Delete(test.testSetup.electionNode, 0)
	if err != nil {
		test.error.Printf("leaderResignationTest: Error deleting election node! %v", err)
		return err
	}

	// SUCCESS
	return nil
}

// NewZKLeaderResignationTest creates a new happy path test case
func NewZKLeaderResignationTest(setup ZKTestSetup) *ZKTest {
	leaderResignationTest := zkLeaderResignationTest{
		testSetup: setup,
		info:      log.New(os.Stderr, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile),
		error:     log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile),
	}
	zkHappyTest := ZKTest{
		Desc:      "Leader Resignation Test",
		Callbacks: &leaderResignationTest,
		Err:       nil,
	}

	return &zkHappyTest
}
