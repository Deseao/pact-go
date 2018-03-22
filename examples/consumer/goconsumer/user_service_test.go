package goconsumer

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pact-foundation/pact-go/dsl"
	"github.com/pact-foundation/pact-go/types"
)

// Common test data
var dir, _ = os.Getwd()
var pactDir = fmt.Sprintf("%s/../../pacts", dir)
var logDir = fmt.Sprintf("%s/log", dir)
var pact dsl.Pact
var form url.Values
var rr http.ResponseWriter
var req *http.Request

// var name = "Jean-Marie de La Beaujardière😀😍"
var name = "billy"
var like = dsl.Like
var eachLike = dsl.EachLike
var term = dsl.Term
var loginRequest = fmt.Sprintf(`{ "username":"%s", "password": "issilly" }`, name)

var commonHeaders = dsl.MatcherMap{
	"Content-Type": dsl.Term("application/json; charset=utf-8", `application\/json`),
}

// Use this to control the setup and teardown of Pact
func TestMain(m *testing.M) {
	// Setup Pact and related test stuff
	setup()

	// Run all the tests
	code := m.Run()

	// Shutdown the Mock Service and Write pact files to disk
	pact.WritePact()
	pact.Teardown()

	// Enable when running E2E/integration tests before a release
	if os.Getenv("PACT_INTEGRATED_TESTS") != "" {
		var brokerHost = os.Getenv("PACT_BROKER_HOST")

		// Publish the Pacts...
		p := dsl.Publisher{}
		err := p.Publish(types.PublishRequest{
			PactURLs:        []string{filepath.FromSlash(fmt.Sprintf("%s/billy-bobby.json", pactDir))},
			PactBroker:      brokerHost,
			ConsumerVersion: "1.0.1",
			Tags:            []string{"latest", "sit4"},
			BrokerUsername:  os.Getenv("PACT_BROKER_USERNAME"),
			BrokerPassword:  os.Getenv("PACT_BROKER_PASSWORD"),
		})

		if err != nil {
			log.Println("ERROR: ", err)
		}
	} else {
		log.Println("Skipping publishing")
	}

	os.Exit(code)
}

// Setup common test data
func setup() {
	pact = createPact()

	// Login form values
	form = url.Values{}
	form.Add("username", "billy")
	form.Add("password", "issilly")

	// Create a request to pass to our handler.
	req, _ = http.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.PostForm = form

	// Record response (satisfies http.ResponseWriter)
	rr = httptest.NewRecorder()
}

// Create Pact connecting to local Daemon
func createPact() dsl.Pact {
	pactDaemonPort := 6666
	return dsl.Pact{
		Port:     pactDaemonPort,
		Consumer: "billy",
		Provider: "bobby",
		LogDir:   logDir,
		PactDir:  pactDir,
	}
}

func TestPactConsumerLoginHandler_UserExists(t *testing.T) {
	var testBillyExists = func() error {
		client := Client{
			Host: fmt.Sprintf("http://localhost:%d", pact.Server.Port),
		}
		client.loginHandler(rr, req)

		// Expect User to be set on the Client
		if client.user == nil {
			return errors.New("Expected user not to be nil")
		}

		return nil
	}
	body :=
		like(fmt.Sprintf(
			`{
            "user": {
              "name": "%s",
              "type": %v
            }
					}`, name, term("admin", "admin|user|guest")))

	// Pull from pact broker, used in e2e/integrated tests for pact-go release
	// Setup interactions on the Mock Service. Note that you can have multiple
	// interactions
	pact.
		AddInteraction().
		Given("User billy exists").
		UponReceiving("A request to login with user 'billy'").
		WithRequest(dsl.Request{
			Method: "POST",
			Path:   dsl.Term("/users/login/1", "/users/login/[0-9]+"),
			Query: dsl.MatcherMap{
				"foo": dsl.Term("bar", "[a-zA-Z]+"),
			},
			Body: loginRequest,
		}).
		WillRespondWith(dsl.Response{
			Status: 200,
			Body:   body,
		})

	err := pact.Verify(testBillyExists)
	if err != nil {
		t.Fatalf("Error on Verify: %v", err)
	}
}

func TestPactConsumerLoginHandler_UserDoesNotExist(t *testing.T) {
	var testBillyDoesNotExists = func() error {
		client := Client{
			Host: fmt.Sprintf("http://localhost:%d", pact.Server.Port),
		}
		client.loginHandler(rr, req)

		if client.user != nil {
			return fmt.Errorf("Expected user to be nil but got: %v", client.user)
		}

		return nil
	}

	pact.
		AddInteraction().
		Given("User billy does not exist").
		UponReceiving("A request to login with user 'billy'").
		WithRequest(dsl.Request{
			Method:  "POST",
			Path:    "/users/login/10",
			Body:    loginRequest,
			Headers: commonHeaders,
			Query: dsl.MatcherMap{
				"foo": "anything",
			},
		}).
		WillRespondWith(dsl.Response{
			Status:  404,
			Headers: commonHeaders,
		})

	err := pact.Verify(testBillyDoesNotExists)
	if err != nil {
		t.Fatalf("Error on Verify: %v", err)
	}
}

func TestPactConsumerLoginHandler_UserUnauthorised(t *testing.T) {
	var testBillyUnauthorized = func() error {
		client := Client{
			Host: fmt.Sprintf("http://localhost:%d", pact.Server.Port),
		}
		client.loginHandler(rr, req)

		if client.user != nil {
			return fmt.Errorf("Expected user to be nil but got: %v", client.user)
		}

		return nil
	}

	pact.
		AddInteraction().
		Given("User billy is unauthorized").
		UponReceiving("A request to login with user 'billy'").
		WithRequest(dsl.Request{
			Method:  "POST",
			Path:    "/users/login/10",
			Body:    loginRequest,
			Headers: commonHeaders,
		}).
		WillRespondWith(dsl.Response{
			Status:  401,
			Headers: commonHeaders,
		})

	err := pact.Verify(testBillyUnauthorized)
	if err != nil {
		t.Fatalf("Error on Verify: %v", err)
	}
}
