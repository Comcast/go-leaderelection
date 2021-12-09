// The goal of this mock worker is to simulate the behavior of a client who is the leader
// crashing and a follower client picking up the same work.
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Comcast/go-leaderelection"
	"github.com/go-zookeeper/zk"
)

const (
	heartBeatTimeout = 2 * time.Second
	zkServerAddr     = "localhost:2181"
	electionNode     = "/leaderCrashTest"
)

var (
	expectToBeLeader = true
)

func main() {
	fmt.Println(len(os.Args), os.Args)

	if len(os.Args) != 2 {
		fmt.Println("Usage: go run mock_worker.go <leader/follower>")
		os.Exit(-1)
	}

	switch os.Args[1] {
	case "leader":
		expectToBeLeader = true
	case "follower":
		expectToBeLeader = false
	default:
		fmt.Println("Only 'leader' and 'follower' supported for arguments")
		os.Exit(-1)
	}

	zkConn, _, err := zk.Connect([]string{zkServerAddr}, heartBeatTimeout)

	if err != nil {
		log.Fatalf("zkLeaderCrashTest: Error in zk.Connect (%s): %v",
			zkServerAddr, err)
	}

	currElection, err := leaderelection.NewElection(zkConn, electionNode, "myhostname")

	if err != nil {
		log.Fatalf("zkLeaderCrashTest: Error in NewElection (%s): %v",
			electionNode, err)
		os.Exit(-1)
	}

	done := make(chan struct{})
	go func() {
		currElection.ElectLeader()
		done <- struct{}{}
	}()
	status := <-currElection.Status()
	if status.Err != nil {
		log.Fatalf("zkLeaderCrashTest: Error in ElectLeader: %v", err)
		os.Exit(-1)
	}

	if expectToBeLeader {
		if status.Role != leaderelection.Leader {
			log.Fatalf("zkLeaderCrashTest: Expected to be leader but isn't")
			os.Exit(-1)
		}
	} else {
		if status.Role != leaderelection.Follower {
			log.Fatalf("zkLeaderCrashTest: Expected to be follower but isn't")
			os.Exit(-1)
		}
	}

	// Do the work if you are the leader
	for count := 0; count < 6; count++ {
		time.Sleep(1 * time.Second)
		if expectToBeLeader {
			log.Printf("main() - leader: Working...")
		} else {

			log.Println("main() - follower: Waiting to become the leader...")
			status := <-currElection.Status()
			if status.Role != leaderelection.Leader {
				log.Fatalf("follower: Didn't become leader as expected! Got: %v",
					status)
				os.Exit(-1)
			}

			time.Sleep(1 * time.Second)

			// Resign from the election
			currElection.Resign()
			<-done
			// This is the only time we return a success
			os.Exit(0)
		}
	}

	// We don't expect to complete running this task
	os.Exit(-1)
}
