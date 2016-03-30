# Overview
This document describes the high-level design of the leader election library.

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
For the leader election examples in this package this node is called <code>/election</code>. For an actual application elections may be defined as `/election-type`. Using politics as an example, the `election-type` might be `president`.
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

# Scenarios
These scenarios cover interactions between leader candidates and the library.

## General startup scenarios
1. ZK not available at startup
	1. The client will be responsible for providing the ZK connection to the library.
		1. Rationale - The client should be in control of the connection so that it can decide whether connections should be reused or not, and what to do if the connection fails in any way.
        1. Rationale - The client will need to interact directly with Zookeeper (ZK) to create the election root path and the full path of the resource which needs a leader.
	1. If the connection is not usable the Library initialization will fail.
1. Start up with no "election" node
    1. The client is now responsible for creating the root of the election path (e.g., /recon)
		1. The client will be responsible for creating the full election node path. E.g., For the Recon Agent this means that the RA will have to monitor the /recon node directly for IRIDs being added. Only when it has the /recon/IRID can it use the Library for leadership election. An ABEER instance will create the /recon/IRID node when a request is made to reconstitute a recording.
1. ZK dies/connection lost
	1. ZK notifies the Client via connection channel
		1. As per #1 above, the Client owns the connection, not the library.
	2. The Library Client is itself monitoring the ZK connection it provided to the Library and as a result of the error `Resign`s its leadership (or leadership request) and closes the Library.
	3. The connection to ZK can be lost for several reasons including:
		1. The network connection between ZK and the client process (e.g., RA) is lost
		1. The ZK instance that the client is connected to becomes partitioned from the other ZKs in the cluster  and it's on the minority side of the partition (ie., no quorum).
1. ZK recovers/connection established
	1. This is the Client's responsibility to implement a strategy appropriate to the client.


## Leader election scenarios
1. Initialization
	1. Client creates ZK connection.
    1. Client creates the ZK election resource (e.g., /some-election-id)
	1. Client initializes the Library with a ZK connection and the election resource.
	1. The result will be a new instance of the Election.
1. Request leadership for a Resource.
	1. The Client requests leadership of the resource separately from initialization. In fact, it may be that the client creating the election is separate from the client(s) that compete for the election.
	1. The library will write an ephemeral, sequenced, znode to ZK representing the leadership request.
	1. The library performs a Get on the resource children.
		1. If this client's request has the lowest sequence number then this client has leadership for the resource.
		1. If this client's request does not have the lowest sequence number then it is not leader.
        1. The Client then places a watch on the client with the next lowest sequence number.
		    1. The Library will place a watch on the request with the next lowest sequence number to avoid the "herd effect" if the leader exits unexpectedly.
	1. The Library returns the following to the Client:
		1. A boolean indicating true if the Client is the leader, false otherwise.
        1. An error reflecting whatever error may have occurred, nil otherwise.
		1. The Library instance struct includes a channel to monitor for changes in leadership status.
			1. This channel will be used when a Client is transitioned from follower to leader for the Resource.
            1. The channel will be nil for leaders.
1. Leadership change - This occurs when a Leader is removed from it's leadership position. This can happen via an explicit resignation or via a failure of the Leader (e.g., network partition)
	1. Library detects change via the channel returned as part of registering a ZK watch
	2. Library initiates leader election process outlined in #2 above, starting at step #2.
		1. The key difference between an initial leader election and one triggered via a leadership change is that the the election process doesn't start with writing a znode for the client, it will already exist.
		1. A naive implementation of leader election may not include going through leader election again, preferring to use the knowledge that a given client has the lowest sequence number in a local cache to determine the leader. That approach has a problem though in that a follower will not know a priori how many prior nodes have failed. It may be that it could be the leader even though it was several places down in the follower sequence. E.g., for candidates 1, 2, 3, and 4 where the leader is 1 and the follower we're interested in is 4, if 3 fails it's possible that 1 and 2 also failed. If 4 doesn't refresh the list of children then it will miss that not only 3 failed, but 1 & 2 also. In this scenario it won't become leader unless it completely refreshes its cache.
    1. There are 2 scenarios for leadership change:
        1. The Candidate receiving the notification is watching the current leader
            * In this case the Candidate receiving the notification will become the leader.
        1. The Candidate is not watching the current leader. In this case the Candidate will not receive any notification as the Candidate that it is following did not exit or otherwise cause its associated Candidate ZK node to be removed.
1. Resign leadership or nomination
	1. The Client explicitly requests leadership resignation via the API
	2. The Library will remove the associated znode from ZK
	3. A new  leader will be chosen as described by #3 above.
	1. The Library will release any resources it holds, reset all fields to their zero values, and be generally unusable (ie., will throw an error if the instance is attempted to be reused). At this point, depending on the context it was created in, this Library instance is eligible for garbage collection.
1. The election is deleted via the API
	1. All Candidates, including the Leader, are notified that the election has ended.
	1. At this point the election instance is unusable. All resources are released, all fields are reset to their zero values, and further attempts to use the instance will result in an error being returned to the client.
1. Query Library state
	1. The client can query the library to determine its leadership role, and the its associated Candidate information.

# Design details
1. Since the Client owns the ZK connection, it can control how many ZK connections it has active. One ZK connection can support multiple Client leadership nominations.
1. There will be an Election instance for each Resource the Client is interested in leading/owning. There will be one ZK child watch per Election instance (i.e., the Client this client is following).

# Concerns
1. How to handle connection lost then recovered
    1. **TEST:** Recovers to another server in the cluster
        1. Just need to make sure that it reconnects successfully
        1. Leaders maintain leadership
        1. Followers are still following
        1. Subsequent leadership election works
    1. **DOCUMENT:** Resign not reflected in ZK due to connection loss
        1. On reconnect the work doesn't have an active leader since ZK still reflects that the previous owner, which didn't resign properly, is no longer active but the leader re-election wasn't triggered because the resignee's node is still in ZK. If the *zombie* zk node for the failed resignee is deleted the leadership election is triggered.
        1. The approach will be to check for zkConn.Delete() error and return error to client
    1. **TODO:** Resign worked, but leader election didn't happen before the connection was lost (or it didn't work). Ideas for fixing include:
        1. Check if there was a leader re-election failure talking to ZK (or did the election notification get lost altogether?)
        1. Periodically re-run leader election?
    1. **DOCUMENT:** Reconnection continues endlessly
        1. There doesn't seem to be a way to limit the number of retries the go-zk library performs to reconnect to ZK.
            1. Ask a question on the issues list. Otherwise:
                1. Fail everything on the first disconnect and close the connection
                1. Let it continue until it succeeds. But what to do about notifying current leaders that they are no longer leaders (because it's been too long)?
                1. Implement custom reconnect code at the application level.
                1. In any case, do I need to keep track of outstanding ephemeral nodes to perhaps delete? I.e., resign candidacy and then go through a new election for every candidate.
        1. The Client will be responsible for implementing the retry strategy (see **Connectivity Loss** section below for more details).
