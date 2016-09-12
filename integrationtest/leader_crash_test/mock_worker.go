// The goal of this mock worker is to simulate the behavior of a client who is the leader
// crashing and a follower client picking up the same work.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/samuel/go-zookeeper/zk"

	"golang.org/x/net/context"

	"github.comcast.com/viper-cog/clog"
	"github.comcast.com/viper-cog/goutil/leaderelection"
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

	ctx := context.Background()

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
		clog.Errorf(clog.FuncStr()+"zkLeaderCrashTest: Error in zk.Connect (%s): %v",
			zkServerAddr, err)
		os.Exit(-1)
	}

	currElection, err := leaderelection.NewElection(zkConn, electionNode, "myhostname")

	if err != nil {
		clog.Errorf(clog.FuncStr()+"zkLeaderCrashTest: Error in NewElection (%s): %v",
			electionNode, err)
		os.Exit(-1)
	}

	done := make(chan struct{})
	go func() {
		currElection.ElectLeader(ctx)
		done <- struct{}{}
	}()
	status := <-currElection.Status()
	if status.Err != nil {
		clog.Errorf(clog.FuncStr()+"zkLeaderCrashTest: Error in ElectLeader: %v", err)
		os.Exit(-1)
	}

	if expectToBeLeader {
		if status.Role != leaderelection.Leader {
			clog.Errorf(clog.FuncStr() + "zkLeaderCrashTest: Expected to be leader but isn't")
			os.Exit(-1)
		}
	} else {
		if status.Role != leaderelection.Follower {
			clog.Errorf(clog.FuncStr() + "zkLeaderCrashTest: Expected to be follower but isn't")
			os.Exit(-1)
		}
	}

	// Do the work if you are the leader
	for count := 0; count < 6; count++ {
		time.Sleep(1 * time.Second)
		if expectToBeLeader {
			fmt.Println("leader: Working...")
			clog.Infof(clog.FuncStr() + "leader: Working...")
		} else {

			fmt.Println("follower: Waiting to become the leader...")
			clog.Infof(clog.FuncStr() + "follower: Waiting to become the leader...")
			status := <-currElection.Status()
			if status.Role != leaderelection.Leader {
				clog.Errorf(clog.FuncStr()+"follower: Didn't become leader as expected! Got: %v",
					status)
				os.Exit(-1)
			}

			time.Sleep(1 * time.Second)

			// Resign from the election
			currElection.Resign(ctx)
			<-done
			// This is the only time we return a success
			os.Exit(0)
		}
	}

	// We don't expect to complete running this task
	os.Exit(-1)
}
