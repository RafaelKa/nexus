package router

import (
	"fmt"
	"testing"
	"time"

	"github.com/gammazero/nexus/wamp"
)

const testRealm = wamp.URI("nexus.test.realm")

func newTestRouter() Router {
	const (
		autoRealm = false
		strictURI = false

		anonAuth      = true
		allowDisclose = false
	)
	r := NewRouter(autoRealm, strictURI)
	r.AddRealm(testRealm, anonAuth, allowDisclose)
	return r
}

func handShake(r Router, client, server wamp.Peer) error {
	client.Send(&wamp.Hello{Realm: testRealm})
	if err := r.Attach(server); err != nil {
		return err
	}

	if len(client.Recv()) != 1 {
		return fmt.Errorf("Expected 1 message in the handshake, received %d",
			len(client.Recv()))
	}

	msg := <-client.Recv()
	if msg.MessageType() != wamp.WELCOME {
		return fmt.Errorf("expected %v, got %v", wamp.WELCOME,
			msg.MessageType())
	}
	return nil
}

func TestHandshake(t *testing.T) {
	client, server := LinkedPeers()
	r := newTestRouter()
	defer r.Close()
	err := handShake(r, client, server)
	if err != nil {
		t.Fatal(err)
	}

	client.Send(&wamp.Goodbye{})
	select {
	case <-time.After(time.Millisecond):
		t.Fatal("no goodbye message after sending goodbye")
	case msg := <-client.Recv():
		if _, ok := msg.(*wamp.Goodbye); !ok {
			t.Fatal("expected GOODBYE, received: ", msg.MessageType())
		}
	}
}

func TestHandshakeBadRealm(t *testing.T) {
	r := NewRouter(false, false)
	defer r.Close()

	client, server := LinkedPeers()

	client.Send(&wamp.Hello{Realm: "does.not.exist"})
	err := r.Attach(server)
	if err == nil {
		t.Error(err)
	}

	if len(client.Recv()) != 1 {
		t.Fatal("Expected one message in the handshake, received ",
			len(client.Recv()))
	}

	msg := <-client.Recv()
	if msg.MessageType() != wamp.ABORT {
		t.Error("Expected ABORT after handshake")
	}
}

func TestRouterSubscribe(t *testing.T) {
	const testTopic = wamp.URI("some.uri")

	sub, subServer := LinkedPeers()
	r := newTestRouter()
	defer r.Close()
	err := handShake(r, sub, subServer)
	if err != nil {
		t.Fatal(err)
	}

	subscribeID := wamp.GlobalID()
	sub.Send(&wamp.Subscribe{Request: subscribeID, Topic: testTopic})

	var subscriptionID wamp.ID
	select {
	case <-time.After(time.Millisecond):
		t.Fatal("Timed out waiting for SUBSCRIBED")
	case msg := <-sub.Recv():
		subMsg, ok := msg.(*wamp.Subscribed)
		if !ok {
			t.Fatal("Expected SUBSCRIBED, got: ", msg.MessageType())
		}
		if subMsg.Request != subscribeID {
			t.Fatal("wrong request ID")
		}
		subscriptionID = subMsg.Subscription
	}

	pub, pubServer := LinkedPeers()
	handShake(r, pub, pubServer)
	pubID := wamp.GlobalID()
	pub.Send(&wamp.Publish{Request: pubID, Topic: testTopic})

	select {
	case <-time.After(time.Millisecond):
		t.Fatal("Timed out waiting for EVENT")
	case msg := <-sub.Recv():
		event, ok := msg.(*wamp.Event)
		if !ok {
			t.Fatal("Expected EVENT, got: ", msg.MessageType())
		}
		if event.Subscription != subscriptionID {
			t.Fatal("wrong subscription ID")
		}
	}
}

func TestPublishAcknowledge(t *testing.T) {
	client, server := LinkedPeers()
	r := newTestRouter()
	defer r.Close()
	err := handShake(r, client, server)
	if err != nil {
		t.Fatal(err)
	}

	id := wamp.GlobalID()
	client.Send(&wamp.Publish{
		Request: id,
		Options: map[string]interface{}{"acknowledge": true},
		Topic:   "some.uri"})

	select {
	case <-time.After(time.Millisecond):
		t.Fatal("sent acknowledge=true, timed out waiting for PUBLISHED")
	case msg := <-client.Recv():
		pub, ok := msg.(*wamp.Published)
		if !ok {
			t.Fatal("sent acknowledge=true, expected PUBLISHED, got: ",
				msg.MessageType())
		}
		if pub.Request != id {
			t.Fatal("wrong request id")
		}
	}
}

func TestPublishFalseAcknowledge(t *testing.T) {
	client, server := LinkedPeers()
	r := newTestRouter()
	defer r.Close()
	err := handShake(r, client, server)
	if err != nil {
		t.Fatal(err)
	}

	id := wamp.GlobalID()
	client.Send(&wamp.Publish{
		Request: id,
		Options: map[string]interface{}{"acknowledge": false},
		Topic:   "some.uri"})

	select {
	case <-time.After(time.Millisecond):
	case msg := <-client.Recv():
		if _, ok := msg.(*wamp.Published); ok {
			t.Fatal("Sent acknowledge=false, but received PUBLISHED: ",
				msg.MessageType())
		}
	}
}

func TestPublishNoAcknowledge(t *testing.T) {
	client, server := LinkedPeers()
	r := newTestRouter()
	defer r.Close()
	err := handShake(r, client, server)
	if err != nil {
		t.Fatal(err)
	}

	id := wamp.GlobalID()
	client.Send(&wamp.Publish{Request: id, Topic: "some.uri"})
	select {
	case <-time.After(time.Millisecond):
	case msg := <-client.Recv():
		if _, ok := msg.(*wamp.Published); ok {
			t.Fatal("Sent acknowledge=false, but received PUBLISHED: ",
				msg.MessageType())
		}
	}
}

func TestRouterCall(t *testing.T) {
	const testProcedure = wamp.URI("nexus.test.endpoint")
	callee, calleeServer := LinkedPeers()
	r := newTestRouter()
	defer r.Close()
	err := handShake(r, callee, calleeServer)
	if err != nil {
		t.Fatal(err)
	}

	registerID := wamp.GlobalID()
	// Register remote procedure
	callee.Send(&wamp.Register{Request: registerID, Procedure: testProcedure})

	var registrationID wamp.ID
	select {
	case <-time.After(10 * time.Millisecond):
		t.Fatal("Timed out waiting for REGISTERED")
	case msg := <-callee.Recv():
		registered, ok := msg.(*wamp.Registered)
		if !ok {
			t.Fatal("expected REGISTERED,got: ", msg.MessageType())
		}
		if registered.Request != registerID {
			t.Fatal("wrong request ID")
		}
		registrationID = registered.Registration
	}

	caller, callerServer := LinkedPeers()
	caller.Send(&wamp.Hello{Realm: testRealm})
	if err := r.Attach(callerServer); err != nil {
		t.Fatal("Error connecting caller")
	}
	if msg := <-caller.Recv(); msg.MessageType() != wamp.WELCOME {
		t.Fatal("expected first message to be ", wamp.WELCOME)
	}
	callID := wamp.GlobalID()
	// Call remote procedure
	caller.Send(&wamp.Call{Request: callID, Procedure: testProcedure})

	var invocationID wamp.ID
	select {
	case <-time.After(10 * time.Millisecond):
		t.Fatal("Timed out waiting for INVOCATION")
	case msg := <-callee.Recv():
		invocation, ok := msg.(*wamp.Invocation)
		if !ok {
			t.Fatal("expected INVOCATION, got: ", msg.MessageType())
		}
		if invocation.Registration != registrationID {
			t.Fatal("wrong registration id")
		}
		invocationID = invocation.Request
	}

	// Returns result of remove procedure
	callee.Send(&wamp.Yield{Request: invocationID})

	select {
	case <-time.After(time.Millisecond):
		t.Fatal("Timed out waiting for RESULT")
	case msg := <-caller.Recv():
		result, ok := msg.(*wamp.Result)
		if !ok {
			t.Fatal("expected RESULT, got ", msg.MessageType())
		}
		if result.Request != callID {
			t.Fatal("wrong result ID")
		}
	}
}