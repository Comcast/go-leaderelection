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

// Package integrationtest is used to run the leaderelection library
// through a series tests with a real zookeeper instance that is
// killed, stop, restarted, etc.
package integrationtest

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/samuel/go-zookeeper/zk"

	"github.com/Comcast/go-leaderelection"
	"github.com/Comcast/goint"
)

const (
	zkServerAddr     = "localhost:2181"
	heartBeatTimeout = 2 * time.Second
)

// Test setup for the ZK Happy Path test case
var ZKHappyPathTestSetup = ZKTestSetup{
	zkURL:        zkServerAddr,
	heartBeat:    heartBeatTimeout,
	electionNode: "/happypathelection",
}

// Test setup for the ZK Host Name test case
var ZKClientNameTestSetup = ZKTestSetup{
	zkURL:        zkServerAddr,
	heartBeat:    heartBeatTimeout,
	electionNode: "/hostNameTest",
}

// Test setup for the ZK Leader Crash test case
var ZKLCTestSetup = ZKTestSetup{
	zkURL:        zkServerAddr,
	heartBeat:    heartBeatTimeout,
	electionNode: "/leaderCrashTest",
}

// Test setup for the ZK Leader Resignation test case
var ZKLRTestSetup = ZKTestSetup{
	zkURL:        zkServerAddr,
	heartBeat:    heartBeatTimeout,
	electionNode: "/leaderResignationElection",
}

// Test setup for the ZK Follower Resignation test case
var ZKFRTestSetup = ZKTestSetup{
	zkURL:        zkServerAddr,
	heartBeat:    heartBeatTimeout,
	electionNode: "/followerResignationElection",
}

// Test setup for the ZK Absence test case
var ZKAbsenceTestSetup = ZKTestSetup{
	zkURL:        zkServerAddr,
	heartBeat:    heartBeatTimeout,
	electionNode: "/zkAbsence",
}

// Test setup for the ZK Absence Part 2 test case
var ZKAbsencePart2TestSetup = ZKTestSetup{
	zkURL:        zkServerAddr,
	heartBeat:    heartBeatTimeout,
	electionNode: "/zkAbsencePart2",
}

// Test setup for the ZK Absence Part 3 test case
var ZKAbsencePart3TestSetup = ZKTestSetup{
	zkURL:        zkServerAddr,
	heartBeat:    heartBeatTimeout,
	electionNode: "/zkAbsencePart3",
}

// Test setup for the ZK Election Node deletion test case
var ZKElectionDeletionTestSetup = ZKTestSetup{
	zkURL:        zkServerAddr,
	heartBeat:    heartBeatTimeout,
	electionNode: "/zkElectionDeletion",
}

// Test setup for the missing ZK Election Node deletion test case
var ZKMissingElectionDeletionTestSetup = ZKTestSetup{
	zkURL:        zkServerAddr,
	heartBeat:    heartBeatTimeout,
	electionNode: "/zkMissingElectionDeletion",
}

// Test setup for the delete Election race test case
var ZKDeleteElectionRaceTestSetup = ZKTestSetup{
	zkURL:        zkServerAddr,
	heartBeat:    heartBeatTimeout,
	electionNode: "/zkDeleteElectionRace",
}

// NOTE: If new entries are added to the the different test setups,
// remember to clean up the election nodes in cleanupElectionNodes()

// This is the list of all the test cases we would like to run
var zkTestCases []goint.Worker

var (
	Error *log.Logger
)

func createLoggers(errorHandle io.Writer) {
	Error = log.New(errorHandle,
		"ERROR: ",
		log.Ldate|log.Ltime|log.Lshortfile)
}

// Each individual test has a bunch of typical steps that need to be
// performed. For instance, setting up test data, running Zookeeper,
// performing the test and stopping zookeeper and do any other cleanups.
// addTest does all of the above for each test case.
func addTest(test *ZKTest, rootDir string) {
	testCase := &goint.Comp{
		Name: test.Desc,
		SubN: []goint.Worker{
			&goint.GoFunc{
				Name: "Create Test Data",
				Func: func(w *goint.GoFunc) {
					w.Err = test.Callbacks.CreateTestData()
				},
			},
			&goint.Exec{
				Name:      "Start Zookeeper Server",
				Token:     "zk start",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "start")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Delay{
				Name: "Wait for Zookeeper Server",
				// Need to do this wait so that ZK can get rid of any previous ephemeral
				// nodes that don't have a valid session associated with them. Note: We
				// assume here that the ephemeral nodes were created with a session timeout
				// that is less than the duration specified here.
				Duration: time.Second * 6,
			},
			test,
			&goint.Exec{
				Name:      "Stop Zookeeper Server",
				Token:     "zk stop",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "stop")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Wait{
				Name:  "Wait Zookeeper Server",
				Token: "zk stop",
			},
		},
	}

	zkTestCases = append(zkTestCases, testCase)
}

func addLeaderCrashTestCase(rootDir string) {

	crashTestCase := &goint.Comp{
		Name: "Leader Crash Test",
		SubN: []goint.Worker{
			&goint.Exec{
				Name:      "Start Zookeeper Server",
				Token:     "zk start",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "start")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Delay{
				Name: "Wait for Zookeeper Server",
				// Need to do this wait so that ZK can get rid of any previous ephemeral
				// nodes that don't have a valid session associated with them. Note: We
				// assume here that the ephemeral nodes were created with a session timeout
				// that is less than the duration specified here.
				Duration: time.Second * 6,
			},
			&goint.GoFunc{
				Name: "Create Election Node",
				Func: func(w *goint.GoFunc) {
					// Create the connection to Zookeeper
					zkConn, _, err := zk.Connect([]string{ZKLCTestSetup.zkURL}, ZKLCTestSetup.heartBeat)

					if err != nil {
						Error.Printf("addLeaderCrashTestCase: leaderCrashTest: Error in zk.Connect (%s): %v",
							ZKLCTestSetup.zkURL, err)
						w.Err = err
						return
					}

					// Create the election node in ZooKeeper
					_, err = zkConn.Create(ZKLCTestSetup.electionNode,
						[]byte("data"), 0, zk.WorldACL(zk.PermAll))
					if err != nil {
						Error.Printf("addLeaderCrashTestCase: leaderCrashTest: Error creating the election node (%s): %v",
							ZKLCTestSetup.electionNode, err)
						w.Err = err
						return
					}
				},
			},
			&goint.Exec{
				Name:      "Run Leader Process",
				Token:     "RunLeaderProcess",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("./mock_worker", "leader")
				},
				Stdout: goint.TailReportOnError,
				Stderr: goint.TailReportOnError,
			},
			&goint.Delay{
				Name:     "Delay before starting Follower Process",
				Duration: 2 * time.Second,
			},
			&goint.Exec{
				Name:       "Run Follwer Process",
				Token:      "RunFollowerProcess",
				Directory:  rootDir,
				Sequential: false,
				Command: func() *exec.Cmd {
					return exec.Command("./mock_worker", "follower")
				},
				Stdout: goint.TailReportOnError,
				Stderr: goint.TailReportOnError,
			},
			&goint.Delay{
				Name:     "Delay before killing Leader Process",
				Duration: 2 * time.Second,
			},
			&goint.Kill{
				Name:  "Kill Leader Process",
				Token: "RunLeaderProcess",
			},
			// Wait for the Follower Process to exit and check the return value
			&goint.Wait{
				Name:  "Wait for Follower Process to exit",
				Token: "RunFollowerProcess",
			},
			&goint.GoFunc{
				Name: "Destroy Election Node",
				Func: func(w *goint.GoFunc) {
					// Create the connection to Zookeeper
					zkConn, _, err := zk.Connect([]string{ZKLCTestSetup.zkURL}, ZKLCTestSetup.heartBeat)

					if err != nil {
						Error.Printf("addLeaderCrashTestCase: leaderCrashTest: Error in zk.Connect (%s): %v",
							ZKLCTestSetup.zkURL, err)
						w.Err = err
						return
					}

					// Destroy the election node in ZooKeeper
					err = zkConn.Delete(ZKLCTestSetup.electionNode, 0)
					if err != nil {
						children, _, _ := zkConn.Children(ZKLCTestSetup.electionNode)
						Error.Printf("addLeaderCrashTestCase: leaderCrashTest:Error deleting election node %v", err)
						w.Err = fmt.Errorf("Error removing node %s, children %sv", err.Error(), children)
						return
					}
				},
			},
			&goint.Exec{
				Name:      "Stop Zookeeper Server",
				Token:     "zk stop",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "stop")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Wait{
				Name:  "Wait Zookeeper Server",
				Token: "zk stop",
			},
		},
	}

	zkTestCases = append(zkTestCases, crashTestCase)
}

func addZKAbsenceTestCase(rootDir string) {
	// Create the connection to Zookeeper and persist this variable
	// through out this test.
	zkConn, _, err := zk.Connect([]string{ZKAbsenceTestSetup.zkURL},
		ZKAbsenceTestSetup.heartBeat)

	crashTestCase := &goint.Comp{
		Name: "ZK Absence Test Case",
		SubN: []goint.Worker{
			&goint.Exec{
				Name:      "Start Zookeeper Server",
				Token:     "zk start",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "start")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Delay{
				Name:     "Wait for Zookeeper Server to Start",
				Duration: time.Second * 6,
			},
			&goint.GoFunc{
				Name: "Create Election Node",
				Func: func(w *goint.GoFunc) {
					// Create the election node in ZooKeeper
					_, err = zkConn.Create(ZKAbsenceTestSetup.electionNode,
						[]byte("data"), 0, zk.WorldACL(zk.PermAll))
					if err != nil {
						Error.Printf("zkAbsenceTest: Error creating the election node (%s): %v",
							ZKAbsenceTestSetup.electionNode, err)
						w.Err = err
						return
					}
				},
			},
			&goint.Exec{
				Name:      "Stop Zookeeper Server stop",
				Token:     "zk stop",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "stop")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Wait{
				Name:  "Wait Zookeeper Server stop",
				Token: "zk stop",
			},
			&goint.Delay{
				Name:     "Wait for Zookeeper Server to exit",
				Duration: time.Second * 6,
			},
			&goint.GoFunc{
				Name: "Test New Election API when ZK Absent",
				Func: func(w *goint.GoFunc) {
					// Call New election
					elector, err := leaderelection.NewElection(zkConn, ZKAbsenceTestSetup.electionNode, "myhostname")
					if err == nil {
						Error.Printf("zkAbsenceTest: Expected Error from NewElection(%s). But it worked!",
							ZKAbsenceTestSetup.electionNode)
						w.Err = fmt.Errorf("zkAbsenceTest: Didn't get error from NewElection as expected")
						return
					}

					// Also, we expect the elector to be nil @ this point
					if elector != nil {
						Error.Printf("zkAbsenceTest: Expected elector to be nil for NewElection(%s)",
							ZKAbsenceTestSetup.electionNode)
						w.Err = fmt.Errorf("zkAbsenceTest: Expected elector to be nil for NewElection(%s)",
							ZKAbsencePart2TestSetup.electionNode)
						return
					}
				},
			},
		},
	}

	zkTestCases = append(zkTestCases, crashTestCase)
}

//Test ZK Absence (ElectLeader and Resign APIs) after election was created
func addZKAbsencePart2TestCase(rootDir string) {
	// Create the connection to Zookeeper and persist this variable
	// through out this test.
	zkConn, _, err := zk.Connect([]string{ZKAbsencePart2TestSetup.zkURL},
		ZKAbsencePart2TestSetup.heartBeat)
	var elector *leaderelection.Election

	crashTestCase := &goint.Comp{
		Name: "ZK AbsencePart2 Test Case",
		SubN: []goint.Worker{
			&goint.Exec{
				Name:      "Start Zookeeper Server",
				Token:     "zk start",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "start")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Delay{
				Name:     "Wait for Zookeeper Server to Start",
				Duration: time.Second * 6,
			},
			&goint.GoFunc{
				Name: "Create Election Node and Invoke election API",
				Func: func(w *goint.GoFunc) {
					// Create the election node in ZooKeeper
					_, err = zkConn.Create(ZKAbsencePart2TestSetup.electionNode,
						[]byte("data"), 0, zk.WorldACL(zk.PermAll))
					if err != nil {
						Error.Printf("zkAbsencePart2Test: Error creating the election node (%s): %v",
							ZKAbsencePart2TestSetup.electionNode, err)
						w.Err = err
						return
					}

					// Call New election API
					elector, err = leaderelection.NewElection(zkConn,
						ZKAbsencePart2TestSetup.electionNode, "myhostname")
					if err != nil {
						Error.Printf("zkAbsencePart2Test: Got error for NewElection(%s)!",
							ZKAbsencePart2TestSetup.electionNode)
						w.Err = fmt.Errorf("zkAbsencePart2Test: Got error for NewElection")
						return
					}
				},
			},
			&goint.Exec{
				Name:      "Stop Zookeeper Server stop",
				Token:     "zk stop",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "stop")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Wait{
				Name:  "Wait Zookeeper Server stop",
				Token: "zk stop",
			},
			&goint.Delay{
				Name:     "Wait for Zookeeper Server to exit",
				Duration: time.Second * 6,
			},
			&goint.GoFunc{
				Name: "Test New Election API when ZK Absent",
				Func: func(w *goint.GoFunc) {

					// Call Elect leader
					go elector.ElectLeader()
					status := <-elector.Status()
					if status.Err == nil {
						Error.Printf("zkAbsencePart2Test: Expected Error from ElectLeader(%s). But it worked!",
							ZKAbsencePart2TestSetup.electionNode)
						w.Err = fmt.Errorf("zkAbsencePart2Test: Didn't get error from ElectLeader as expected")
					}
				},
			},
		},
	}

	zkTestCases = append(zkTestCases, crashTestCase)
}

//Test ZK Absence - Test what happens when after a election is complete and
// leader is working and follower is following and ZK stops. The client/application
// doesn't get notified asynchronously but any ZK related API requests will RETURN
// error to the application synchronously.
func addZKAbsencePart3TestCase(rootDir string) {
	// Create the connection to Zookeeper and persist this variable
	// through out this test.
	zkConn, _, err := zk.Connect([]string{ZKAbsencePart2TestSetup.zkURL},
		ZKAbsencePart2TestSetup.heartBeat)
	var leaderElector, followerElector *leaderelection.Election

	crashTestCase := &goint.Comp{
		Name: "ZK AbsencePart3 Test Case",
		SubN: []goint.Worker{
			&goint.Exec{
				Name:      "Start Zookeeper Server",
				Token:     "zk start",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "start")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Delay{
				Name:     "Wait for Zookeeper Server to Start",
				Duration: time.Second * 6,
			},
			&goint.GoFunc{
				Name: "Create Election Node and complete the election process",
				Func: func(w *goint.GoFunc) {
					// Create the election node in ZooKeeper
					_, err = zkConn.Create(ZKAbsencePart3TestSetup.electionNode,
						[]byte("data"), 0, zk.WorldACL(zk.PermAll))
					if err != nil {
						Error.Printf("zkAbsencePart3Test: Error creating the election node (%s): %v",
							ZKAbsencePart3TestSetup.electionNode, err)
						w.Err = err
						return
					}

					// Create new elector for leader
					leaderElector, err = leaderelection.NewElection(zkConn,
						ZKAbsencePart3TestSetup.electionNode, "myhostname")
					if err != nil {
						Error.Printf("zkAbsencePart3Test: Got error for NewElection(%s)!",
							ZKAbsencePart3TestSetup.electionNode)
						w.Err = fmt.Errorf("zkAbsencePart3Test: Got error for NewElection")
						return
					}

					// Call Elect leader
					go leaderElector.ElectLeader()
					status := <-leaderElector.Status()
					if status.Err != nil {
						Error.Printf("zkAbsencePart3Test: Got error from ElectLeader(%s)(ldr)",
							ZKAbsencePart3TestSetup.electionNode)
						w.Err = fmt.Errorf("zkAbsencePart3Test: Got error from ElectLeader(%s)(ldr)",
							ZKAbsencePart3TestSetup.electionNode)
						return
					}

					// Create new elector for follower
					followerElector, err = leaderelection.NewElection(zkConn,
						ZKAbsencePart3TestSetup.electionNode, "myhostname")
					if err != nil {
						Error.Printf("zkAbsencePart3Test: Got error for NewElection(%s)!",
							ZKAbsencePart3TestSetup.electionNode)
						w.Err = fmt.Errorf("zkAbsencePart3Test: Got error for NewElection")
						return
					}

					// Call Elect leader
					go followerElector.ElectLeader()
					fStatus := <-followerElector.Status()
					if fStatus.Err != nil {
						Error.Printf("zkAbsencePart3Test: Got error from ElectLeader(%s)(flwr)",
							ZKAbsencePart3TestSetup.electionNode)
						w.Err = fmt.Errorf("zkAbsencePart3Test: Got error from ElectLeader(%s)(flwr)",
							ZKAbsencePart3TestSetup.electionNode)
						return
					}

					// Make sure Leader and Follower are as expected
					if status.Role != leaderelection.Leader || fStatus.Role != leaderelection.Follower {
						Error.Printf("zkAbsencePart3Test: Leader and Follower are not as expected!")
						w.Err = fmt.Errorf("zkAbsencePart3Test: Leader and Follower are not as expected")
						return
					}
				},
			},
			&goint.Delay{
				Name:     "Wait for a bit",
				Duration: time.Second * 2,
			},
			&goint.Exec{
				Name:      "Stop Zookeeper Server stop",
				Token:     "zk stop",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "stop")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Wait{
				Name:  "Wait Zookeeper Server stop",
				Token: "zk stop",
			},
			&goint.GoFunc{
				Name: "Check notification channel",
				Func: func(w *goint.GoFunc) {
					// Note: We do not expect to get any notification from the LE library
					// at this point in a steady state. Invoking any ZK related API call
					// would return an error back @ this point.
					if followerElector == nil {
						return
					}
					followerElector.Resign()
					select {
					case s, ok := <-followerElector.Status():
						if ok {
							w.Err = fmt.Errorf("zkAbsencePart3Test: follower sent status %s after resign", s.String())
							return
						}
					case <-time.After(2 * time.Second):
						w.Err = fmt.Errorf("zkAbsencePart3Test: Follower did not close election channel within 2 seconds of resign")
					}

					// NOTE: If there are other APIs that will return an error,
					// we can check that here.
				},
			},
		},
	}

	zkTestCases = append(zkTestCases, crashTestCase)
}

func addZKElectionNodeDeleteTestCase(rootDir string) {
	// Create the connection to Zookeeper and persist this variable
	// through out this test.
	zkConn, _, err := zk.Connect([]string{ZKElectionDeletionTestSetup.zkURL},
		ZKElectionDeletionTestSetup.heartBeat)
	var leaderElector, followerElector *leaderelection.Election

	crashTestCase := &goint.Comp{
		Name: "ZK Election Node Deletion Test case",
		SubN: []goint.Worker{
			&goint.Exec{
				Name:      "Start Zookeeper Server",
				Token:     "zk start",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "start")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Delay{
				Name:     "Wait for Zookeeper Server to Start",
				Duration: time.Second * 6,
			},
			&goint.GoFunc{
				Name: "Create Election Node",
				Func: func(w *goint.GoFunc) {
					// Create the election node in ZooKeeper
					_, err = zkConn.Create(ZKElectionDeletionTestSetup.electionNode,
						[]byte("data"), 0, zk.WorldACL(zk.PermAll))
					if err != nil {
						Error.Printf("ElecNodeDeletionTest: Error creating the election node (%s): %v",
							ZKElectionDeletionTestSetup.electionNode, err)
						w.Err = err
						return
					}
				},
			},
			&goint.GoFunc{
				Name: "Create leader and follower",
				Func: func(w *goint.GoFunc) {
					// Create New election
					leaderElector, err = leaderelection.NewElection(zkConn,
						ZKElectionDeletionTestSetup.electionNode, "myhostname")
					if err != nil {
						Error.Printf("ElecNodeDeletionTest: Error creating a New Elector for leader(%s)",
							ZKElectionDeletionTestSetup.electionNode)
						w.Err = fmt.Errorf("ElecNodeDeletionTest: Error creating a New Election (leader)")
						return
					}

					// Elect leader
					go leaderElector.ElectLeader()
					status := <-leaderElector.Status()
					if status.Err != nil {
						Error.Printf("ElecNodeDeletionTest: Error Electing a leader!")
						w.Err = fmt.Errorf("ElecNodeDeletionTest: Error Electing a leader!")
						return
					}

					// Create New election
					followerElector, err = leaderelection.NewElection(zkConn,
						ZKElectionDeletionTestSetup.electionNode, "myhostname")
					if err != nil {
						Error.Printf("ElecNodeDeletionTest: Error creating a New Elector for follower (%s)",
							ZKElectionDeletionTestSetup.electionNode)
						w.Err = fmt.Errorf("ElecNodeDeletionTest:Error creating a New Election(follower)")
						return
					}

					// Elect follower
					go followerElector.ElectLeader()
					fStatus := <-followerElector.Status()
					if fStatus.Err != nil {
						Error.Printf("ElecNodeDeletionTest: Error Electing a follower!")
						w.Err = fmt.Errorf("ElecNodeDeletionTest: Error Electing a follower!")
						return
					}

					// Make sure Leader and Follower are as expected
					if status.Role != leaderelection.Leader || fStatus.Role != leaderelection.Follower {
						Error.Printf("ElecNodeDeletionTest: Leader and Follower are not as expected!")
						w.Err = fmt.Errorf("ElecNodeDeletionTest:Leader and Follower are not as expected")
						return
					}
				},
			},
			&goint.Exec{
				Name:      "Delete Election Node",
				Token:     "Delete Election Node",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "removeZKNode.sh",
						ZKElectionDeletionTestSetup.electionNode)
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.GoFunc{
				Name: "Test behavior of leader and follower",
				Func: func(w *goint.GoFunc) {
					// Check if the leader got notified of an error.
					select {
					case status := <-leaderElector.Status():
						if status.Err == nil {
							w.Err = fmt.Errorf("Got unexpected status (%v) from channel for %s!",
								status, status.CandidateID)
							return
						}
					case <-time.After(2 * time.Second):
						w.Err = fmt.Errorf("Didn't receive any notification on the channel for leader!")
						return
					}

					// Check if the follower got notified of an error.
					select {
					case fStatus := <-followerElector.Status():
						if fStatus.Err == nil {
							w.Err = fmt.Errorf("Got unexpected status (%v) from channel for %s!",
								fStatus, fStatus.CandidateID)
							return
						}
					case <-time.After(2 * time.Second):
						w.Err = fmt.Errorf("Didn't receive any notification on the channel for follower!")
						return
					}
				},
			},
			&goint.Exec{
				Name:      "Stop Zookeeper Server stop",
				Token:     "zk stop",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "stop")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Wait{
				Name:  "Wait Zookeeper Server stop",
				Token: "zk stop",
			},
		},
	}

	zkTestCases = append(zkTestCases, crashTestCase)
}

func cleanupElectionNodes(rootDir string) {
	cleanupElectionNodes := &goint.Comp{
		Name: "Clean up Election Nodes",
		SubN: []goint.Worker{
			&goint.Exec{
				Name:      "Start Zookeeper Server",
				Token:     "zk start",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "start")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Delay{
				Name: "Wait for Zookeeper Server",
				// Need to do this wait so that ZK can get rid of any previous ephemeral
				// nodes that don't have a valid session associated with them. Note: We
				// assume here that the ephemeral nodes were created with a session timeout
				// that is less than the duration specified here.
				Duration: time.Second * 6,
			},
			&goint.GoFunc{
				Name: "Clean up Nodes",
				Func: func(w *goint.GoFunc) {
					// Create the connection to Zookeeper
					zkConn, _, err := zk.Connect([]string{zkServerAddr}, heartBeatTimeout)

					if err != nil {
						Error.Printf("cleanupElectionNodes: Error in zk.Connect (%s): %v",
							zkServerAddr, err)
						w.Err = err
						return
					}

					// Remove the election nodes in Zookeeper; ignore errors since
					// the node may not exist.
					zkConn.Delete(ZKHappyPathTestSetup.electionNode, 0)
					zkConn.Delete(ZKClientNameTestSetup.electionNode, 0)
					zkConn.Delete(ZKLCTestSetup.electionNode, 0)
					zkConn.Delete(ZKLRTestSetup.electionNode, 0)
					zkConn.Delete(ZKFRTestSetup.electionNode, 0)
					zkConn.Delete(ZKAbsenceTestSetup.electionNode, 0)
					zkConn.Delete(ZKAbsencePart2TestSetup.electionNode, 0)
					zkConn.Delete(ZKAbsencePart3TestSetup.electionNode, 0)
					zkConn.Delete(ZKElectionDeletionTestSetup.electionNode, 0)
					// Note: the following line is only present so that it can be explained that the associated
					// ZK node will never be created for the associated test.
					//zkConn.Delete(ZKMissingElectionDeletionTestSetup.electionNode, 0)
					zkConn.Delete(ZKDeleteElectionRaceTestSetup.electionNode, 0)
				},
			},
			&goint.Delay{
				Name:     "Wait for Zookeeper Server to delete nodes",
				Duration: time.Second * 2,
			},
			&goint.Exec{
				Name:      "Stop Zookeeper Server",
				Token:     "zk stop",
				Directory: rootDir,
				Command: func() *exec.Cmd {
					return exec.Command("sh", "zookeeper.sh", "stop")
				},
				Stdout: goint.TailReportOn,
				Stderr: goint.TailReportOn,
			},
			&goint.Wait{
				Name:  "Wait Zookeeper Server",
				Token: "zk stop",
			},
		},
	}

	zkTestCases = append(zkTestCases, cleanupElectionNodes)
}

func createTests(rootDir string) {
	cleanupElectionNodes(rootDir)

	// Add all the different tests we want to run
	zkHappyPathTest := NewZKHappyPathTest(ZKHappyPathTestSetup)
	addTest(zkHappyPathTest, rootDir)

	// Add the host name test
	zkClientNameTest := NewZKClientNameTest(ZKClientNameTestSetup)
	addTest(zkClientNameTest, rootDir) // Add the crash test case to be run next
	addLeaderCrashTestCase(rootDir)

	// Add the ZK absence test case - Test NewElection API when ZK is down
	addZKAbsenceTestCase(rootDir)

	// Add the ZK absence Part2 test case - Test ElectLeader and Resign APIs when ZK is down
	addZKAbsencePart2TestCase(rootDir)
	// Add the ZK absence Part3 test case - Test notification channel when ZK goes down
	// after the election is complete and worker is working and follower is following
	addZKAbsencePart3TestCase(rootDir)

	// Leader resignation test where we expect a follower to pick up the work
	// after the leader has resigned.
	zkLeaderResignationTest := NewZKLeaderResignationTest(ZKLRTestSetup)
	addTest(zkLeaderResignationTest, rootDir)
	//
	// Follower resignation test where we test the notification sent to
	// leadership change channel when a follower resigns.
	zkFollowerResignationTest := NewZKFollowerResignationTest(ZKFRTestSetup)
	addTest(zkFollowerResignationTest, rootDir)
	//
	// Tests attempt to delete a non-existent election works as expected.
	zkDelMissingElectionTest := NewZKDeleteMissingElectionTest(ZKMissingElectionDeletionTestSetup)
	addTest(zkDelMissingElectionTest, rootDir)
	//
	// Tests attempt to create a race condition during election deletion. The idea is to add a large number
	// of candidates while the election is being deleted. The result should be that the election is deleted.
	// Otherwise, the election won't be deleted and will have children (candidates).
	zkDelElectionRaceTest := NewZKDeleteElectionRaceTest(ZKDeleteElectionRaceTestSetup)
	addTest(zkDelElectionRaceTest, rootDir)

	// Add the ZK Election deletion test case
	addZKElectionNodeDeleteTestCase(rootDir)
}

// TestMain kicks off all the tests
func TestMain(t *testing.T) {
	createLoggers(os.Stderr)
	rootDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Error getting wd: %v", err)
	}

	createTests(rootDir)

	var zkIntgTest = &goint.Comp{
		Name: "Top",
		SubN: []goint.Worker{
			&goint.Comp{
				Name: "Compile Deps",
				SubN: []goint.Worker{
					&goint.Exec{
						Name:      "Leader Election Library",
						Token:     "LeaderElectionLibraryCompile",
						Directory: rootDir,
						Command: func() *exec.Cmd {
							return exec.Command("go", "build", "-v", "-race",
								"github.com/Comcast/go-leaderelection")
						},
						Stdout: goint.TailReportOnError,
						Stderr: goint.TailReportOnError,
					},
					&goint.Wait{
						Name:  "Wait Leader Election Library Compile",
						Token: "LeaderElectionLibraryCompile",
					},
					&goint.Exec{
						Name:      "Remove Leader Crash Test executable before building it",
						Token:     "LeaderCrashTestExecRemove",
						Directory: rootDir,
						Command: func() *exec.Cmd {
							return exec.Command("rm", "-f", "-v", "mock_worker")
						},
						Stdout: goint.TailReportOnError,
						Stderr: goint.TailReportOnError,
					},
					&goint.Wait{
						Name:  "Wait removing leader crash test executable",
						Token: "LeaderCrashTestExecRemove",
					},
					&goint.Exec{
						Name:      "Leader Crash Test Executable",
						Token:     "LeaderCrashTestExecutableCompile",
						Directory: rootDir,
						Command: func() *exec.Cmd {
							return exec.Command("go", "build", "-v", "-race",
								"leader_crash_test/mock_worker.go")
						},
						Stdout: goint.TailReportOnError,
						Stderr: goint.TailReportOnError,
					},
					&goint.Wait{
						Name:  "Wait Leader Crash Test Executable build to finish",
						Token: "LeaderCrashTestExecutableCompile",
					},
				},
			},
			&goint.Comp{
				Name: "TestCases",
				SubN: zkTestCases,
			},
		},
	}

	fmt.Printf("Welcome to Zookeeper integration test\n")

	zkIntgTest.Start()
	if err := zkIntgTest.Report(0); err != nil {
		t.Errorf("Error occured: %v", err)
	}
}
