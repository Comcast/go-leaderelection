# Copyright 2016 Comcast Cable Communications Management, LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# End Copyright

#!/bin/sh
# Script to detect the correct path of Zookeeper Client
# Usage:
#     removeZKNode.sh <node path>
#

print_usage() {
	 echo "Usage: "
	 echo "    removeZKNode.sh <node path>"
}

if [ $# -ne 1 ]
then
	 print_usage
	 exit 1
fi

OP="deleteall $1"

FOUND=`which zkCli`
if [ $? -eq 0 ]; then
	 echo "Zookeper Client found at $FOUND"
	 ZKCLI=$FOUND
	 $ZKCLI $OP
	 exit $?
fi

FOUND=`which zkcli`
if [ $? -eq 0 ]; then
	 echo "Zookeper Client found at $FOUND"
	 ZKCLI=$FOUND
	 $ZKCLI $OP
	 exit $?
fi

# Check Zookeeper Client inside zookeeper lib
ZK_EXEC_PATH="/usr/lib/zookeeper/bin/zkCli.sh"
if [ -x $ZK_EXEC_PATH ]; then
	 echo "Zookeeper Client found at $ZK_EXEC_PATH"
	 ZKCLI=$ZK_EXEC_PATH
	 $ZKCLI $OP
	 exit $?
fi

echo "Zookeeper Client not found!"
exit 1
