# Leader Election
`leaderelection` provides the capability for a set of distributed processes to compete for leadership for a shared resource. It is implemented using Zookeeper for the underlying support. For a general description of leader election, see the [wikipedia page describing leader election](https://en.wikipedia.org/wiki/Leader_election).

# Terms
<table border="1">
<thead>
<tr class="header">
<th align="left"><p>Term</p></th>
<th align="left"><p>Description</p></th>
</tr>
</thead>
<tbody>
</tr>
<tr>
<td align="left"><p>Nominee</p></td>
<td align="left"><p>Nominee is a potential leader. Each client nominates itself. One will become Leader, the others will become <em>Candidates</em>.
</p></td>
<tr>
<td align="left"><p>Candidate</p></td>
<td align="left"><p>See <em>Nominee</em>.
</p></td>
</tr>
<tr>
<td align="left"><p>Client</p></td>
<td align="left"><p>Client is the customer, or client, of the Library (see below). The Client uses the Library to negotiate leadership for a given resource.
</p></td>
</tr>
<tr>
<td align="left"><p>Election Node</p></td>
<td align="left"><p>ElectionNode is the base Zookeeper node that leader candidates will place their nominations for leadership.
For the leader election examples in this package this node is called <code>/election</code>. For an actual application elections may be defined as `/election-type`. Using politics as an example, the `election-type` might be `president`. <p>An application that has the concept of a *primary* and *standby* `election-type` might be *primary*. For applications that have multiple components the *Election Node* might have 2 or more Zookeeper nodes. An example might be `/component-type/primary` where multiple application components have the concept of `primary` and `standby`.</p>
</p></td>
</tr>
<tr>
<td align="left"><p>Resource</p></td>
<td align="left"><p>Resource is the target of the leadership request. If leadership is granted the leader is free to manage the resource in whatever way is appropriate to the application without worrying that other clients are concurrently managing that resource. *Resource* is a more general name for *Election Node*.
</p></td>
</tr>
<tr>
<td align="left"><p>Follower</p></td>
<td align="left"><p>Follower is an entity that is not the leader for a Resource, but is actively monitoring the resource in the event the Leader is removed from its leadership position. If this happens, a Follower becomes a candidate for Leader which will be decided via an automatic, asynchronous, process.
</p></td>
</tr>
<tr>
<td align="left"><p>Leader</p></td>
<td align="left"><p>Leader is the entity that has exclusive ownership for a given Resource.
</p></td>
</tr>
<tr>
<td align="left"><p>Library</p></td>
<td align="left"><p>Library is this component - i.e., the Leader Election library.
</p></td>
</tr>
</tr>
<tr>
<td align="left"><p>ZK</p></td>
<td align="left"><p>Zookeeper.
</p></td>
</tr>
</tbody>
</table>

# How to use

This section provides an overview of the various phases of an election. The file `Election_test.go` is a good example of how a client is expected to use the leader election package. The integration tests (see the `integrationtest directory`) may also provide some insight into how to use the package although their intent is primarily to test the package vs. provide examples.

Leader Election (LE) clients will have to import the following packages:

    import (
    	"github.com/samuel/go-zookeeper/zk"
    	"github.comcast.com/viper-cog/leaderelection"
    )

Leader election clients must provide a ZK connection when creating an election instance. The rationale behind this is to prevent applications that participate in multiple elections from letting the library create multiple ZK connections behind the scenes. This allows the application to optimize the use of ZK connections.

## Create a *Resource*
A *resource* is represented as a ZK node. During the design and implementation of the library an explicit decision was made to not provide the capability in the library to create election resources. This decision may be revisited in the future.

    // Create the ZK connection
    zkConn, _, err := zk.Connect([]string{zkURL}, heartBeat)
    if err != nil {
        log.Printf("Error in zk.Connect (%s): %v",
            zkURL, err)
        return nil, err
    }

    // Create the election node in ZooKeeper
    _, err = zkConn.Create(electionNode,
        []byte(""), 0, zk.WorldACL(zk.PermAll))
    if err != nil {
        log.Printf("Error creating the election node (%s): %v",
            electionNode, err)
        return nil, err
    }

    candidate, err := leaderelection.NewElection(zkConn, electionNode)



## Delete an Election

    err := leaderelection.DeleteElection(zkConn, electionNode)
or

    leaderelection.EndElection()

## Request Leadership for an Election

Leadership election is inherently an asynchronous process. For candidates that win an election the inital response is fairly rapid. Even so, leaders can still receive errors from the election that will impact their leadership of a resource. The most common example is when the underlying election instance gets disconnected from Zookeeper. In the event of errors, leaders expected to resign their leadership. This is likely a futile process if the ZooKeeper connection is lost, but it's good practice to do this for any error.

Followers of an election can wait an indeterminate amount of time to become the leader of an election. If a follower becomes a leader they will be notified. In some cases the election may end before a follower can become a leader. Like leaders, followers can receive errors from the election. Like leaders, they should resign from the election in this case.

Finally, and election can be unilaterally ended by any actor in the application. As with errors, this results in a status notification to all candidates, including the leader. Candidates are expected to resign from the election in this case as well.

Given that leadership election is an asynchronous process clients should start the election as a goroutine as shown below. All events pertaining to the status of an election are communicated via channels. So the typical pattern is for a client to monitor the election status in a `for/select` loop as shown below.

    // Each candidate should begin their participation in the
    // by starting a goroutine.
    go candidate.ElectLeader()
    ...

## Monitor election and candidate's role in the election
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
}


## Resign from an Election
    candidate.Resign()
## Leadership change
See the **Monitor election and candidate's role in the election** above.
## Query status
Candidates are always notified when an election's status changes. It is up to the client to cache this status if they need to reference it between status changes.

# Testing the package
Testing the package requires certain prerequisites be met

1. All tests require the availablility of a Zookeeper installation. `Election_test.go` requires that Zookeeper be running. The integration tests control Zookeeper so Zookeeper should not be running when executing the integration tests.
1. The integration tests leverage `github.com.Comcast.goutil`. This package must be installed prior to executing integration tests.
