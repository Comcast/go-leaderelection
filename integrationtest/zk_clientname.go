package integrationtest

/*
Goal of this test:
  Verify that the user specified client name parameter when we create an Election gets
  set as expected inside the ephemeral node that is created for the client
Steps in this test:
   1. Create a election resource
   2. Create a election client and provide the hostname as the client name
   3. Let this client win the election
   4. Get the value stored in the ephemeral node for this client and make sure
      the payload stored in the ephemeral node is the hostname
   5. Repeat step 2 with the client name being "foobar"
   6. This client would be the follower for this election
   7. Verify that the value stored in the ephemeral node for this follower client
      has the payload matching "foobar"
*/

import (
	"fmt"
	"os"
	"time"

	"github.com/Comcast/go-leaderelection"
	"github.com/samuel/go-zookeeper/zk"

	"log"
)

const (
	defaultClientName = "myclientname"
)

// zkClientNameTest is the struct that controls the test.
type zkClientNameTest struct {
	testSetup       ZKTestSetup
	leaderElector   *leaderelection.Election
	followerElector *leaderelection.Election
	zkConn          *zk.Conn
	done            chan struct{}

	expectedName1 string
	expectedName2 string

	info  *log.Logger
	error *log.Logger
}

// CreateTestData: Implement the zkTestCallbacks interface
func (test *zkClientNameTest) CreateTestData() (err error) {
	return nil
}

// CreateTestData: Implement the zkTestCallbacks interface
// Initialize the steps
func (test *zkClientNameTest) InitFunc() ([]time.Duration, error) {
	zkConn, _, err := zk.Connect([]string{test.testSetup.zkURL}, test.testSetup.heartBeat)
	test.zkConn = zkConn
	test.done = make(chan struct{}, 2)

	if err != nil {
		return nil, err
	}

	// Create the election node in ZooKeeper
	_, err = test.zkConn.Create(test.testSetup.electionNode,
		[]byte("data"), 0, zk.WorldACL(zk.PermAll))
	if err != nil {
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
func (test *zkClientNameTest) StepFunc(idx int) error {
	switch idx {
	case 0: // Create the Election
		test.info.Printf("Step %d: Create Election.", idx)

		test.expectedName1, _ = os.Hostname()
		if test.expectedName1 == "" {
			test.expectedName1 = defaultClientName
		}

		elector, err := leaderelection.NewElection(test.zkConn,
			test.testSetup.electionNode, test.expectedName1)
		test.leaderElector = elector

		if err != nil {
			test.error.Printf("zkClientNameTest: Error in NewElection (%s): %v",
				test.testSetup.electionNode, err)
			return err
		}

	case 1: // Elect the leader
		test.info.Printf("Step %d: Elect Leader", idx)
		go func() {
			test.leaderElector.ElectLeader()
			test.done <- struct{}{}
		}()

		// Expect this to succeed; there is only one candidate
		status := <-test.leaderElector.Status()
		if status.Role != leaderelection.Leader {
			return fmt.Errorf("zkClientNameTest: Expected to be a leader but am not")
		}

		// Check payload to make sure it has the expected string
		data, _, err := test.zkConn.Get(status.CandidateID)

		if err != nil {
			test.info.Printf("zkClientNameTest: Error %v when calling Get on %s",
				err, status.CandidateID)
			return err
		}

		if string(data) != test.expectedName1 {
			test.info.Printf("zkClientNameTest: Expected Client Name: %s, Got: %s",
				test.expectedName1, data)
			return fmt.Errorf("zkClientNameTest: Expected Client Name: %s, Got: %s",
				test.expectedName1, data)
		}

	case 2: // Perform similar test for the follower case
		test.info.Printf("Step %d: Elect Follower", idx)

		test.expectedName2 = "foobar"
		elector, err := leaderelection.NewElection(test.zkConn,
			test.testSetup.electionNode, test.expectedName2)
		test.followerElector = elector

		if err != nil {
			return err
		}

		go func() {
			test.followerElector.ElectLeader()
			test.done <- struct{}{}
		}()

		// Expect to become a follower
		status := <-test.followerElector.Status()
		if status.Role != leaderelection.Follower {
			return fmt.Errorf("zkClientNameTest: Expected to be a follower but am not")
		}

		// Check payload to make sure it has the expected string
		data, _, err := test.zkConn.Get(status.CandidateID)

		if err != nil {
			return err
		}

		if string(data) != test.expectedName2 {
			return fmt.Errorf("zkClientNameTest: Expected Client Name: %s, Got: %s",
				test.expectedName2, data)
		}

	case 3: // Resign
		test.info.Printf("Step %d: Resign", idx)
		test.followerElector.Resign()
		<-test.done
		test.leaderElector.Resign()
		<-test.done
		test.info.Printf("zkClientNameTest: Test Success")
	}
	return nil
}

// Verify the results
func (test *zkClientNameTest) EndFunc() error {
	test.info.Printf("zkClientNameTest: EndFunc(): Verification")

	err := test.zkConn.Delete(test.testSetup.electionNode, 0)
	if err != nil {
		children, _, _ := test.zkConn.Children(test.testSetup.electionNode)
		return fmt.Errorf("zkClientNameTest: Error deleting election node! %v, children: %v", err, children)
	}

	// SUCCESS
	return nil
}

// NewZKClientNameTest creates a new happy path test case
func NewZKClientNameTest(setup ZKTestSetup) *ZKTest {
	clientNameTest := zkClientNameTest{
		testSetup: setup,
		info:      log.New(os.Stderr, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile),
		error:     log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile),
	}
	zkClientNameTest := ZKTest{
		Desc:      "Client Name Test",
		Callbacks: &clientNameTest,
		Err:       nil,
	}

	return &zkClientNameTest
}
