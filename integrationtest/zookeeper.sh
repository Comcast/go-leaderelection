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
# Script to detect the correct path of Zookeeper and start/stop it
# Usage:
#     zookeeper.sh {start|start-foreground|stop|restart|status|upgrade|print-cmd}
#

print_usage() {
	 echo "Usage: "
	 echo "    zookeeper.sh {start|start-foreground|stop|restart|status|upgrade|print-cmd}"
}

if [ $# -ne 1 ]
then
	 print_usage
	 exit 1
fi

case $1 in
	 start|start-foreground|stop|restart|status|upgrade|print-cmd)
		  OP=$1;;
	 *)
		  print_usage
		  exit 1;;
esac


FOUND=`which zkServer`
if [ $? -eq 0 ]; then
	 echo "Zookeper found at $FOUND"
	 ZKSERVER=$FOUND
	 $ZKSERVER $OP
	 exit $?
fi

FOUND=`which zkserver`
if [ $? -eq 0 ]; then
	 echo "Zookeper found at $FOUND"
	 ZKSERVER=$FOUND
	 $ZKSERVER $OP
	 exit $?
fi

# Check Zookeeper inside zookeeper lib
ZK_EXEC_PATH="/usr/lib/zookeeper/bin/zkServer.sh"
if [ -x $ZK_EXEC_PATH ]; then
	 echo "Zookeeper found at $ZK_EXEC_PATH"
	 ZKSERVER=$ZK_EXEC_PATH
	 $ZKSERVER $OP
	 exit $?
fi

echo "Zookeeper not found!"
exit 1
