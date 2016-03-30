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
	"regexp"
	"time"

	"log"

	"os"

	"github.com/samuel/go-zookeeper/zk"
	"github.comcast.com/viper-cog/leaderelection"
)

// zkHappyPathTest is the struct that controls the test.
type zkHappyPathTest struct {
	testSetup     ZKTestSetup
	leaderElector *leaderelection.Election
	zkConn        *zk.Conn
	done          chan struct{}
	info          *log.Logger
	error         *log.Logger
}

// CreateTestData: Implement the zkTestCallbacks interface
func (test *zkHappyPathTest) CreateTestData() (err error) {
	test.info.Println("zkHappyPathTest: CreateTestData()")
	return nil
}

// CreateTestData: Implement the zkTestCallbacks interface
// Initialize the steps
func (test *zkHappyPathTest) InitFunc() ([]time.Duration, error) {
	test.info.Println("zkHappyPathTest: InitFunc()")

	zkConn, _, err := zk.Connect([]string{test.testSetup.zkURL}, test.testSetup.heartBeat)
	test.zkConn = zkConn
	test.done = make(chan struct{})

	if err != nil {
		test.error.Printf("zkHappyPathTest: Error in zk.Connect (%s): %v",
			test.testSetup.zkURL, err)
		return nil, err
	}

	// Create the election node in ZooKeeper
	_, err = test.zkConn.Create(test.testSetup.electionNode,
		[]byte("data"), 0, zk.WorldACL(zk.PermAll))
	if err != nil {
		test.error.Printf("zkHappyPathTest: Error creating the election node (%s): %v",
			test.testSetup.electionNode, err)
		return nil, err
	}

	return []time.Duration{
		time.Second * 1,
		time.Second * 1,
		time.Second * 1,
		time.Second * 1,
	}, nil
}

// CreateTestData: Implement the zkTestCallbacks interface
func (test *zkHappyPathTest) StepFunc(idx int) error {
	switch idx {
	case 0: // Create the Election
		test.info.Printf("Step %d: Create Election.", idx)

		elector, err := leaderelection.NewElection(test.zkConn, test.testSetup.electionNode)
		test.leaderElector = elector

		if err != nil {
			test.error.Printf("zkHappyPathTest: Error in NewElection (%s): %v",
				test.testSetup.electionNode, err)
			return err
		}

		// Additional checks: Make sure the node was created and is an ephemeral node
		nodeExists, nodeStat, err := test.zkConn.Exists(test.testSetup.electionNode)

		if err != nil {
			test.error.Printf("zkHappyPathTest: Error %v when checking for node %s",
				err, test.testSetup.electionNode)
			return err
		}

		if !nodeExists {
			test.error.Printf("zkHappyPathTest: Expected node %s to exist but didn't!",
				test.testSetup.electionNode)
			return fmt.Errorf("zkHappPathTest: Expected node %s to exist but didn't", test.testSetup.electionNode)
		}

		// EphemeralOwner is only set when the node is Ephemeral
		if 0 != nodeStat.EphemeralOwner {
			test.error.Printf("zkHappyPathTest: Election node should not be ephemeral!")
			return fmt.Errorf("zkHappyPathTest: Election node should not be ephemeral")
		}

	case 1: // Elect the leader
		test.info.Printf("Step %d: Elect Leader", idx)
		go func() {
			test.leaderElector.ElectLeader()
			test.done <- struct{}{}
		}()

		// We expect this to succeed since no one is competing for this election
		// excpet this test.
		status := <-test.leaderElector.Status()
		if status.Role != leaderelection.Leader {
			// Print the list of candidates attached to the election node so we know
			// why we failed.
			children, stat, _ := test.zkConn.Children(test.testSetup.electionNode)
			test.info.Printf("List of children: %v, stat(%s): %v\n", children, test.testSetup.electionNode, stat)
			return fmt.Errorf("zkHappyPathTest: Expected to be a leader but am not")
		}

		test.info.Printf("zkHappyPathtest: Candidate %v became the leader for Election: %s",
			status.CandidateID,
			test.leaderElector)

		// Additional checks: Make sure the leader node is ephemeral and has a sequence number
		nodeExists, nodeStat, err := test.zkConn.Exists(status.CandidateID)

		if err != nil {
			test.error.Printf("zkHappyPathTest: Error %v when checking for node %s",
				err, status.CandidateID)
			return err
		}

		if !nodeExists {
			test.error.Printf("zkHappyPathTest: Expected node %s to exist but didn't!",
				status.CandidateID)
			return fmt.Errorf("zkHappPathTest: Expected node %s to exist but didn't",
				status.CandidateID)
		}

		// EphemeralOwner is only set when the node is Ephemeral
		// and candidates/leader nodes should be ephemeral
		if 0 == nodeStat.EphemeralOwner {
			test.error.Printf("zkHappyPathTest: Candidate node should be ephemeral")
			return fmt.Errorf("zkHappyPathTest: Candidate node should be ephemeral")
		}

		// The candidate node should have a sequence number set as well.
		candidateRegex := "^" + test.testSetup.electionNode + "/le_" + "([0-9]){10}$"
		r := regexp.MustCompile(candidateRegex)
		match := r.MatchString(status.CandidateID)
		if !match {
			test.error.Printf("zkHappyPathTest: Candidate string didn't match %s .",
				candidateRegex)
			return fmt.Errorf("zkHappyPathTest: Candidate didn't have a sequence number")
		}

	case 2: // Verify that I am the leader
		test.info.Printf("Step %d: Validate Election", idx)
		select {
		case s, ok := <-test.leaderElector.Status():
			return fmt.Errorf("zkHappyPathTest: Got updated leader status %s when not expected channel closed? %v", s.String(), ok)
		case <-time.After(1 * time.Second):
		}

	case 3: // Try resigning and recheck again.
		test.info.Printf("Step %d: Resign and re-check", idx)
		test.leaderElector.Resign()
		<-test.done
		test.info.Printf("zkHappyPathTest: Test Success")
	}
	return nil
}

// CreateTestData: Implement the zkTestCallbacks interface
// Verify the results
func (test *zkHappyPathTest) EndFunc() error {
	test.info.Println("zkHappyPathTest: EndFunc(): Verification")

	err := test.zkConn.Delete(test.testSetup.electionNode, 0)
	if err != nil {
		children, _, _ := test.zkConn.Children(test.testSetup.electionNode)
		return fmt.Errorf("zkHappyPathTest: Error deleting election node! %v, children: %v", err, children)
	}

	// SUCCESS
	return nil
}

// NewZKHappyPathTest creates a new happy path test case
func NewZKHappyPathTest(setup ZKTestSetup) *ZKTest {
	happyPathTest := zkHappyPathTest{
		testSetup: setup,
		info:      log.New(os.Stderr, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile),
		error:     log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile),
	}
	zkHappyTest := ZKTest{
		Desc:      "Happy Path Test",
		Callbacks: &happyPathTest,
		Err:       nil,
	}

	return &zkHappyTest
}
