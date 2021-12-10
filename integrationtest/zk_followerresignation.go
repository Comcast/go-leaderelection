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

// Goal of this test: Start a leader client and a couple of follower clients.
// Have the 1st follower resign and make sure the second one gets notified.
// Have the leader resign and make sure the 2nd follower now becomes the leader.

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Comcast/go-leaderelection"
	"github.com/go-zookeeper/zk"
)

// zkFollowerResignationTest is the struct that controls the test.
type zkFollowerResignationTest struct {
	testSetup                   ZKTestSetup
	leaderElector               *leaderelection.Election
	follower1Elector            *leaderelection.Election
	follower2Elector            *leaderelection.Election
	lStatus, f1Status, f2Status leaderelection.Status
	zkConn                      *zk.Conn
	info                        *log.Logger
	error                       *log.Logger
}

// CreateTestData: Implement the zkTestCallbacks interface
func (test *zkFollowerResignationTest) CreateTestData() (err error) {
	test.info.Println("CreateTestData()")
	return nil
}

// CreateTestData: Implement the zkTestCallbacks interface
// Initialize the steps
func (test *zkFollowerResignationTest) InitFunc() ([]time.Duration, error) {
	test.info.Println("InitFunc()")

	// Create the connection to Zookeeper
	zkConn, _, err := zk.Connect([]string{test.testSetup.zkURL}, test.testSetup.heartBeat)
	test.zkConn = zkConn

	if err != nil {
		test.error.Printf("frTest: Error in zk.Connect (%s): %v",
			test.testSetup.zkURL, err)
		return nil, err
	}

	// Create the election node in ZooKeeper
	myStr, err := zkConn.Create(test.testSetup.electionNode,
		[]byte("data"), 0, zk.WorldACL(zk.PermAll))
	if err != nil {
		test.error.Printf("frTest: Error creating the election node (%s): %v",
			test.testSetup.electionNode, err)
		return nil, err
	}

	fmt.Println("Got myStr from create: ", myStr)

	return []time.Duration{
		time.Second * 1,
		time.Second * 1,
		time.Second * 1,
		time.Second * 1,
		time.Second * 1,
	}, nil
}

// CreateTestData: Implement the zkTestCallbacks interface
func (test *zkFollowerResignationTest) StepFunc(idx int) error {
	var err error
	switch idx {
	case 0: // Create the Election
		test.info.Printf("Step %d: Create Election.", idx)

		leaderElector, err := leaderelection.NewElection(test.zkConn, test.testSetup.electionNode, "myhostname")
		test.leaderElector = leaderElector

		if err != nil {
			test.error.Printf("frTest: Error in NewElection (leader) (%s): %v",
				test.testSetup.electionNode, err)
			return err
		}

		test.follower1Elector, err = leaderelection.NewElection(test.zkConn,
			test.testSetup.electionNode, "myhostname")

		if err != nil {
			test.error.Printf("frTest: Error in NewElection (follower1) (%s): %v",
				test.testSetup.electionNode, err)
			return err
		}

		test.follower2Elector, err = leaderelection.NewElection(test.zkConn,
			test.testSetup.electionNode, "myhostname")

		if err != nil {
			test.error.Printf("frTest: Error in NewElection (follower2) (%s): %v",
				test.testSetup.electionNode, err)
			return err
		}

	case 1: // Elect the leader and add both the followers
		test.info.Printf("Step %d.a: Elect Leader", idx)

		go test.leaderElector.ElectLeader()
		test.lStatus, err = electAndAssert(test.leaderElector, true)
		if err != nil {
			test.error.Printf("frTest: Error in ElectLeader(leader): %v", err)
			return err
		}

		// Make sure leader is not following any one
		if test.lStatus.NowFollowing != "" {
			test.error.Printf("frTest: Leader not expected to follow %s", test.lStatus.NowFollowing)
			return fmt.Errorf("Leader not expected to follow %s", test.lStatus.NowFollowing)
		}

		test.info.Printf("Step %d.b: Add a follower", idx)
		// Make another candidate sign up for the same election
		go test.follower1Elector.ElectLeader()
		test.f1Status, err = electAndAssert(test.follower1Elector, false)
		if err != nil {
			return err
		}

		// Make sure follower 1 is following the leader
		if test.f1Status.NowFollowing != test.lStatus.CandidateID {
			return fmt.Errorf("Follower not following leader(%v) instead foll. %v",
				test.lStatus.CandidateID, test.f1Status.NowFollowing)
		}

		go test.follower2Elector.ElectLeader()
		test.info.Printf("Step %d.c: Add a second follower", idx)
		// Make another candidate sign up for the same election
		test.f2Status, err = electAndAssert(test.follower2Elector, false)
		if err != nil {
			return err
		}

		// Make sure leader is not following any one
		if test.f2Status.NowFollowing != test.f1Status.CandidateID {
			return fmt.Errorf("Second follower not following first(%v) instead foll. %v",
				test.f1Status.CandidateID, test.f2Status.NowFollowing)
		}

	case 2: // Have the follower1 resign, make sure leader is still a leader and follower2 got
		// notified of the follower1 being removed
		test.info.Printf("Step %d: Follower 1 Resign", idx)
		test.follower1Elector.Resign()
		select {
		case s, ok := <-test.follower1Elector.Status():
			if ok {
				return fmt.Errorf("frTest, follower1 did not close its channel on resignation, instead got status %s", s.String())
			}
		case <-time.After(15 * time.Second):
			return fmt.Errorf("frTest, follower1 did not close its channel within 15s of resignation")
		}

		// Follower resigning shouldn't affect the leader in anyway
		select {
		case test.lStatus = <-test.leaderElector.Status():
			return fmt.Errorf("frTest, leader status changed on follower resignation, %+v", test.lStatus)
		case test.f2Status = <-test.follower2Elector.Status():
			if test.f2Status.NowFollowing != test.lStatus.CandidateID {
				return fmt.Errorf("frTest, f2 nowfollowing not equal to leader candidate id after f1 resigned, got %s expected %s", test.f2Status.NowFollowing, test.lStatus.CandidateID)
			}
			if test.f2Status.WasFollowing != test.f1Status.CandidateID {
				return fmt.Errorf("frTest, f2 wasfollowing not equal to f1 candidate id got %s, expected %s", test.f2Status.WasFollowing, test.f1Status.CandidateID)
			}
			if test.f2Status.Role != leaderelection.Follower {
				return fmt.Errorf("frTest, f2 now leader after f1 resigned")
			}
			break
		case <-time.After(15 * time.Second):
			return fmt.Errorf("frTest, follower2 did not change on follower1 resignation")
		}

	case 3: // Make the leader resign and make sure follower 2 becomes the leader
		test.info.Printf("Step %d: Leader resigns", idx)

		test.leaderElector.Resign()

		select {
		case test.f2Status = <-test.follower2Elector.Status():
			// follower2 should be a leader now if everything is working as expected.
			if test.f2Status.Role != leaderelection.Leader {
				return fmt.Errorf("Follower2's isn't leader when expected to be")
			}
			if test.f2Status.NowFollowing != "" {
				return fmt.Errorf("Follower2 shouldn't be following anything now, got %s", test.f2Status.NowFollowing)
			}
			if test.f2Status.WasFollowing != test.lStatus.CandidateID {
				return fmt.Errorf("Follower2 was followeing expected to be leader, got %s", test.f2Status.WasFollowing)
			}
		case <-time.After(10 * time.Second):
			return fmt.Errorf(`frTest: Didn't receive ldrship change notification in time`)
		}

		test.info.Printf("Test Success")
	}
	return nil
}

// CreateTestData: Implement the zkTestCallbacks interface
// Verify the results
func (test *zkFollowerResignationTest) EndFunc() error {
	test.info.Println("EndFunc(): Verification")

	// Have the any child nodes resign so that we can delete the election node
	test.follower2Elector.Resign()
	select {
	case s, ok := <-test.follower2Elector.Status():
		if ok {
			return fmt.Errorf("Follower 2 did not close status channel, instead sent: %s", s.String())
		}
	case <-time.After(2 * time.Second):
		return fmt.Errorf("Follower 2 did not close status channel within 2 seconds")
	}
	if err := test.zkConn.Delete(test.testSetup.electionNode, 0); err != nil {
		test.error.Printf("zkFollowerResignationTest: error deleting election node! %v", err)
		return err
	}

	// SUCCESS
	return nil
}

// NewZKFollowerResignationTest creates a new happy path test case
func NewZKFollowerResignationTest(setup ZKTestSetup) *ZKTest {
	followerResignationTest := zkFollowerResignationTest{
		testSetup: setup,
		info:      log.New(os.Stderr, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile),
		error:     log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile),
	}
	zkHappyTest := ZKTest{
		Desc:      "Follower Resignation Test",
		Callbacks: &followerResignationTest,
		Err:       nil,
	}

	return &zkHappyTest
}

func electAndAssert(elector *leaderelection.Election, isExpectedToBeLeader bool) (leaderelection.Status, error) {
	role := leaderelection.Role(leaderelection.Follower)
	if isExpectedToBeLeader {
		role = leaderelection.Leader
	}

	s := <-elector.Status()

	if s.Role != role {
		return s, fmt.Errorf("Unexpected leadership status %d for candidate %s", s.Role, s.CandidateID)
	}
	return s, nil
}
