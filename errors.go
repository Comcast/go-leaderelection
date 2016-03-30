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

package leaderelection

const (
	// ElectionCompletedNotify indicates the election is over and no new candidates can be nominated.
	ElectionCompletedNotify = "Election is over, no new candidates allowed"
	// ElectionSelfDltNotify indicates that the referenced candidate was deleted. This can happen if the election is
	// ended (EndElection) or deleted (DeleteElection)
	ElectionSelfDltNotify = "Candidate has been deleted"
	// While this candidate was finding who to follow (because it wasn't leader), it became leader. This notification
	// identifies this condition.
	leaderWhileDeterminingWhoToFollow = `While determining who to follow the candidate became leader
	because of a race condition whereby the original leader was deleted after the candidate decided it wasn't leader.`
)
