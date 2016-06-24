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
	"sync"
	"time"

	"github.com/Comcast/goint"
)

// ZKTestSetup - test setup
type ZKTestSetup struct {
	zkURL        string        // Address of Zookeeper server
	heartBeat    time.Duration // Heart beat timeout
	electionNode string        // The election node to use
}

// zkTestCallbacks - archive test interface
type zkTestCallbacks interface {
	// Create Test Data - this function will be called before start zookeeper
	CreateTestData() error

	// InitFunc - test initializiation function. Called before test starts.
	// function returns a list of steps.  Each step is represented by the time duration since
	// last step.
	InitFunc() ([]time.Duration, error)

	// StepFunc - this fuction is called for each step.
	// Index passed in is the index of the step.
	StepFunc(int) error

	// EndFunc - function is called after test is completed.
	EndFunc() error
}

// ZKTest - Zookeeper test control structure, it implements go.int
type ZKTest struct {
	Desc      string
	Callbacks zkTestCallbacks
	QuerySize int   // Default query size for paged query
	Err       error // not nil: error occured during the test
}

// Go - implement goint interface.
func (test *ZKTest) Go(wg *sync.WaitGroup) {
	var (
		ticks []time.Duration
	)
	defer wg.Done()

	ticks, test.Err = test.Callbacks.InitFunc()
	if test.Err != nil {
		return
	}

	for i, delay := range ticks {

		if delay != 0 {
			time.Sleep(delay)
		}

		test.Err = test.Callbacks.StepFunc(i)
		if test.Err != nil {
			return
		}
	}

	test.Err = test.Callbacks.EndFunc()
}

// Report - implement goint inteface
func (test *ZKTest) Report(dep int) error {
	goint.ReportNode(dep, test.Desc, test.Err)
	return test.Err
}
