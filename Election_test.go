package leaderelection

/**
 *	Uncomment the code in this file to run some simple integration tests as described in the //Tests comments on
 *	line 21.
 *
import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	zk "github.com/go-zookeeper/zk"
)

// NOTE!!!!!
// The expected Zookeeper server address is hard-coded near the top of the Test01() function
// END NOTE!!!!!

//Tests:
//	1. DONE: Integration tests cover this.
//		Of multiple candidates, only 1 becomes leader
//		Start multiple goroutines, 1 for each candidate.
//	2. DONE:  Integration tests cover this
//		When a leader of multiple candidates resigns, one of the remaining candidates is
//		chosen as leader.
//		Delete the leader ZK no de.
//	3. DONE:  Integration tests cover this
//		When the last leader resigns there is no leader and no remaining candidates
//		Just let the test run
//	4. DONE: Integration tests cover this
//		When the ZK connection is lost all candidates are notified and exit
//		Kill the ZK process.
// 	5. DONE: Integration tests cover this
//		Delete the election node (e.g., /election or /recon/IRID). All candidates should exit.
//	6. DONE: MANUAL still
// 		Delete the last candidate in list (i.e., the last follower) and make sure it gets the
//		notification and stops processing (i.e., it resigns).
//	7. TODO: How does it work in a distributed (i.e., multi-process/multi-host) environment
//		i. When ZK connection is lost to one ZK in a cluster
//			a. Expect all happy-path scenarios still work
//	8. DONE: Integration tests cover this. Of multiple candidates remove the node for one of the followers (1). Verify that the
//		next immediate follower (2) follows the node prior (0) to the removed follower (1)
//

const numCandidates = 4

type ElectionResponse struct {
	IsLeader    bool
	CandidateID string
}

func TestNilZKConn(t *testing.T) {
	t.Log("Given a nir Zookeeper connection")
	_, err := NewElection(nil, "someResourceString", "myhostname")
	if err == nil {
		t.Fatal("\t\tShould get a non-nil error returned")
	}
	t.Log("\tThe election request is rejected")
}

// This test requires a running ZK server or mocked go-zookeeper library
func TestMissingElectionResource(t *testing.T) {
	t.Log("Given a non-existent election resource")

	conn, _ := connect("127.0.0.1:2181")
	_, err := NewElection(conn, "someResourceString", "myhostname")
	if err == nil {
		t.Fatal("\tExpected the request to fail and it didn't")
	}
	t.Log("\tThe election request is rejected")
}

func BenchmarkTestLeaderElectionAndRecovery(b *testing.B) {
	for i := 0; i < b.N; i++ {
		respCh := make(chan ElectionResponse)
		connFailCh := make(chan bool)
		conn, connEvtChan := connect("127.0.0.1:2181")

		//	clog.EnableDebug()

		go func() {
			for {
				evt := <-connEvtChan
				//			fmt.Println("Received channel event:", evt)
				if evt.State == zk.StateDisconnected {
					close(connFailCh)           // signal candidates to exit
					time.Sleep(2 * time.Second) // give goroutines time to exit
					break
				}
			}

		}()

		createElectionResource(conn)

		var wg sync.WaitGroup
		for i := 0; i < numCandidates; i++ {
			wg.Add(1)
			go runCandidate(conn, "/election", &wg, respCh, uint(i), connFailCh)
		}
		go func() {
			wg.Wait()
			close(respCh)
		}()

		getResponses(respCh)
	}
}


// This test requires a running ZK server or mocked go-zookeeper library

func TestLeaderElectionAndRecovery(t *testing.T) {

	t.Log("Given multiple election candidates verify leader election and leader failure recovery")
	respCh := make(chan ElectionResponse)
	connFailCh := make(chan bool)
	conn, connEvtChan := connect("127.0.0.1:2181")

	//	clog.EnableDebug()

	go func() {
		for {
			evt := <-connEvtChan
			//			fmt.Println("Received channel event:", evt)
			if evt.State == zk.StateDisconnected {
				close(connFailCh)           // signal candidates to exit
				time.Sleep(2 * time.Second) // give goroutines time to exit
				break
			}
		}

	}()

	createElectionResource(conn)

	var wg sync.WaitGroup
	for i := 0; i < numCandidates; i++ {
		wg.Add(1)
		go runCandidate(conn, "/election", &wg, respCh, uint(i), connFailCh)
	}
	go func() {
		wg.Wait()
		close(respCh)
	}()

	responses := getResponses(respCh)

	//	fmt.Println("\n\nCandidates at end:", le.String())
	t.Log("\tWhen verifying results")
	err := verifyResults(responses)
	if err != nil {
		t.Fatal("\t\tGot an invalid result, non-nil error returned", err)
	}
	t.Log("\t\tReceived a valid result")
}


// ------------------- [ Helper functions ] ---------------------------------------//

func connect(zksStr string) (*zk.Conn, <-chan zk.Event) {
	//	zksStr := os.Getenv("ZOOKEEPER_SERVERS")
	zks := strings.Split(zksStr, ",")
	conn, connEvtChan, err := zk.Connect(zks, 5*time.Second)
	must(err)
	return conn, connEvtChan
}

func createElectionResource(conn *zk.Conn) {
	exists, _, _ := conn.Exists("/election")
	var (
		err error
	)
	if !exists {
		flags := int32(0)
		acl := zk.WorldACL(zk.PermAll)
		_, err = conn.Create("/election", []byte("data"), flags, acl)
		must(err)
		//		fmt.Printf("created: %+v\n", path)
	}
}

func getResponses(respCh chan ElectionResponse) []ElectionResponse {
	var responses []ElectionResponse
	var i int
	for response := range respCh {
		//		fmt.Println("Election result", i, ":", response)
		i++
		responses = append(responses, response)
	}
	return responses
}

func must(err error) {
	if err != nil {
		//		fmt.Println("must(", err, ") called")
		panic(err)
	}
}

func runCandidate(zkConn *zk.Conn, electionPath string, wg *sync.WaitGroup,
	respCh chan ElectionResponse, waitFor uint, connFailCh chan bool) {

	leaderElector, err := NewElection(zkConn, "/election", "myhostname")
	if err != nil {
		fmt.Println("Error creating a new election: <", err, ">. Abandoning election.")
		wg.Done()
		return
	}

	go leaderElector.ElectLeader()

	var status Status
	var ok bool

	for {
		select {
		case status, ok = <-leaderElector.Status():
			if !ok {
				fmt.Println("\t\t\tChannel closed, election is terminated!!!")
				respCh <- ElectionResponse{false, status.CandidateID}
				leaderElector.Resign()
				wg.Done()
				return
			}
			if status.Err != nil {
				fmt.Println("Received election status error <<", status.Err, ">> for candidate <", leaderElector.candidateID, ">.")
				leaderElector.Resign()
				wg.Done()
				return
			}

			fmt.Println("Candidate received status message: <", status, ">.")
			if status.Role == Leader {
				doLeaderStuff(leaderElector, status, respCh, connFailCh, waitFor)
				wg.Done()
				return
			}
		case <-connFailCh:
			fmt.Println("\t\t\tZK connection failed for candidate <", status.CandidateID, ">, exiting")
			respCh <- ElectionResponse{false, status.CandidateID}
			leaderElector.Resign()
			wg.Done()
			return
		case <-time.After(time.Second * 100):
			fmt.Println("\t\t\tERROR!!! Timer expired, stop waiting to become leader for", status.CandidateID)
			leaderElector.Resign()
			respCh <- ElectionResponse{false, status.CandidateID}
			wg.Done()
			return
		}
		fmt.Println("Will accessing leaderElector.candidateID <", leaderElector.candidateID, "> result in a race error?")

	}
}

func doLeaderStuff(leaderElector *Election, status Status, respCh chan ElectionResponse, connFailCh chan bool, waitFor uint) {
	respCh <- ElectionResponse{true, status.CandidateID}
	if !strings.EqualFold(status.NowFollowing, "") {
		fmt.Println("\t\t\tLeader", status.CandidateID, "is unexpectedly following <",
			status.NowFollowing, ">")
		//TODO: Fix panic below
		panic("Test failed!!!! Please please please find a better way to signal test failure")
	}
	fmt.Println("\tCandidate <", status.CandidateID, "> is Leader.")

	// do some work when I become leader
	select {
	case <-connFailCh:
		fmt.Println("\t\t\tZK connection failed while LEADING, candidate <", status.CandidateID, "> resigning.")
		leaderElector.Resign()
		return
	case status, ok := <-leaderElector.status:
		if !ok {
			fmt.Println("\t\t\tChannel closed while Leading, candidate <", status.CandidateID, "> resigning.")
			leaderElector.Resign()
			return
		} else if status.Err != nil {
			fmt.Println("\t\t\tError detected in whil leading, candidate <", status.CandidateID, "> resigning. Error was <",
				status.Err, ">.")
			leaderElector.Resign()
			return
		} else {
			fmt.Println("\t\t\tUnexpected status notification while Leading, candidate <", status.CandidateID, "> resigning.")
			leaderElector.Resign()
			return
		}
	default:
		// does (almost) no work, uncommenting next block will simulate longer work
	//	case <-time.NewTimer(1 * time.Second).C:
	//		// Work is done, move on...
	}

	fmt.Print("\t\t\tCandidate <", status.CandidateID, "> has completed its work, it is resigning.\n\n\n")
	//	leaderElector.EndElection()
	leaderElector.Resign()
	return
}

func verifyResults(responses []ElectionResponse) error {
	testPassed := true
	numResponses := 0
	for _, leaderResp := range responses {
		fmt.Println("Received ElectionResponse <", leaderResp, ">")
		//		if leaderResp.IsLeader != true {
		//			//			fmt.Println("Test failed!!!! for candidate:", leaderResp)
		//			testPassed = false
		//			break
		//		}
		numResponses++
	}
	if numResponses != numCandidates {
		//		fmt.Println("Test failed!!! Expected", numCandidates, "responses but received", numResponses)
		testPassed = false
	}
	if !testPassed {
		//		fmt.Println("\nTEST FAILED, unexpected failed election or wrong number of leaders elected")
		return fmt.Errorf("Test failed!!! Expected (%d) responses but received (%d)", numCandidates, numResponses)
	}
	//		fmt.Println("\nTEST PASSED, HOORAY!!!")
	return nil
}

*/
