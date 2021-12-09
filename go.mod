module github.com/youngkin/go-leaderelection

go 1.15

require (
	github.com/Comcast/go-leaderelection v0.0.0-20181102191523-272fd9e2bddc
	github.com/Comcast/goint v0.0.0-20160331154011-38be0f9824d5
	github.com/go-zookeeper/zk v1.0.2
	golang.org/x/net v0.0.0-20211208012354-db4efeb81f4b
)

replace github.com/Comcast/go-leaderelection => ../go-leaderelection
