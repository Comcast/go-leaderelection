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
	"log"
	"time"

	"fmt"

	"os"

	"github.com/Comcast/go-leaderelection"
	"github.com/go-zookeeper/zk"
)

type zkDeleteMissingElectionTest struct {
	testSetup     ZKTestSetup
	leaderElector *leaderelection.Election
	zkConn        *zk.Conn
	done          chan struct{}
	info          *log.Logger
}

func (test *zkDeleteMissingElectionTest) CreateTestData() (err error) {
	test.info.Printf("zkDeleteMissingElectionTest: CreateTestData()")
	return nil
}

func (test *zkDeleteMissingElectionTest) InitFunc() ([]time.Duration, error) {
	test.info.Println("zkDeleteMissingElectionTest: InitFunc()")

	return []time.Duration{
		time.Millisecond * 1,
	}, nil
}

func (test *zkDeleteMissingElectionTest) StepFunc(idx int) error {
	switch idx {
	case 0: // Delete the a non-existent election via the library
		test.info.Printf("Step %d: Delete Missing Election", idx)
		zkConn, _, err := zk.Connect([]string{test.testSetup.zkURL}, test.testSetup.heartBeat)

		if err != nil {
			return fmt.Errorf("Error creating zk connection: <%v>", err)
		}

		err = leaderelection.DeleteElection(zkConn, test.testSetup.electionNode)
		if err != nil {
			return fmt.Errorf("%s Unexpected error received from leaderelection.DeleteElection: %v", "zkDeleteMissingElectionTest", err)
		}
	}
	return nil
}

func (test *zkDeleteMissingElectionTest) EndFunc() error {
	// Nothing to do
	return nil
}

// NewZKHappyPathTest creates a new happy path test case
func NewZKDeleteMissingElectionTest(setup ZKTestSetup) *ZKTest {
	deleteMissingElectionTest := zkDeleteMissingElectionTest{
		testSetup: setup,
		info:      log.New(os.Stderr, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile),
	}
	zkMissingElectionTest := ZKTest{
		Desc:      "Delete Missing Election Test",
		Callbacks: &deleteMissingElectionTest,
		Err:       nil,
	}

	return &zkMissingElectionTest
}
