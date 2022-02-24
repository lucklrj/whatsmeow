module github.com/lucklrj/whatsmeow/mdtest

go 1.17

require (
	github.com/mattn/go-sqlite3 v1.14.11
	github.com/mdp/qrterminal/v3 v3.0.0
	github.com/lucklrj/whatsmeow v0.0.0-20220215120744-a1550ccceb70
	google.golang.org/protobuf v1.27.1
)

require (
	filippo.io/edwards25519 v1.0.0-rc.1 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/lucklrj/libsignal v0.0.0-20211109153248-a67163214910 // indirect
	golang.org/x/crypto v0.0.0-20220214200702-86341886e292 // indirect
	rsc.io/qr v0.2.0 // indirect
)

replace github.com/lucklrj/whatsmeow => ../
