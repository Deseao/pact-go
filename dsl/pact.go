/*
Package dsl contains the main Pact DSL used in the Consumer
collaboration test cases, and Provider contract test verification.
*/
package dsl

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/logutils"
	"github.com/pact-foundation/pact-go/types"
	"github.com/pact-foundation/pact-go/utils"
)

// Pact is the container structure to run the Consumer Pact test cases.
type Pact struct {
	// Current server for the consumer.
	Server *types.MockServer

	// Port the Pact Daemon is running on.
	Port int

	// Pact RPC Client.
	pactClient *PactClient

	// Consumer is the name of the Consumer/Client.
	Consumer string

	// Provider is the name of the Providing service.
	Provider string

	// Interactions contains all of the Mock Service Interactions to be setup.
	Interactions []*Interaction

	// Log levels.
	LogLevel string

	// Used to detect if logging has been configured.
	logFilter *logutils.LevelFilter

	// Location of Pact external service invocation output logging.
	// Defaults to `<cwd>/logs`.
	LogDir string

	// Pact files will be saved in this folder.
	// Defaults to `<cwd>/pacts`.
	PactDir string

	// PactFileWriteMode specifies how to write to the Pact file, for the life
	// of a Mock Service.
	// "overwrite" will always truncate and replace the pact after each run
	// "update" will append to the pact file, which is useful if your tests
	// are split over multiple files and instantiations of a Mock Server
	// See https://github.com/realestate-com-au/pact/blob/master/documentation/configuration.md#pactfile_write_mode
	PactFileWriteMode string

	// Specify which version of the Pact Specification should be used (1 or 2).
	// Defaults to 2.
	SpecificationVersion int

	// Host is the address of the Daemon, Mock and Verification Service runs on
	// Examples include 'localhost', '127.0.0.1', '[::1]'
	// Defaults to 'localhost'
	Host string

	// Network is the network of the Daemon, Mock and Verification Service
	// Examples include 'tcp', 'tcp4', 'tcp6'
	// Defaults to 'tcp'
	Network string

	// Ports MockServer can be deployed to, can be CSV or Range with a dash
	// Example "1234", "12324,5667", "1234-5667"
	AllowedMockServerPorts string
}

// AddInteraction creates a new Pact interaction, initialising all
// required things. Will automatically start a Mock Service if none running.
func (p *Pact) AddInteraction() *Interaction {
	p.Setup(true)
	log.Printf("[DEBUG] pact add interaction")
	i := &Interaction{}
	p.Interactions = append(p.Interactions, i)
	return i
}

// Setup starts the Pact Mock Server. This is usually called before each test
// suite begins. AddInteraction() will automatically call this if no Mock Server
// has been started.
func (p *Pact) Setup(startMockServer bool) *Pact {
	log.Printf("[DEBUG] pact setup1")
	p.setupLogging()
	log.Printf("[DEBUG] pact setup2")
	dir, _ := os.Getwd()

	if p.Network == "" {
		p.Network = "tcp"
	}

	if p.Host == "" {
		p.Host = "localhost"
	}

	if p.LogDir == "" {
		p.LogDir = fmt.Sprintf(filepath.Join(dir, "logs"))
	}

	if p.PactDir == "" {
		p.PactDir = fmt.Sprintf(filepath.Join(dir, "pacts"))
	}

	if p.SpecificationVersion == 0 {
		p.SpecificationVersion = 2
	}

	if p.pactClient == nil {
		client := &PactClient{Port: p.Port, Network: p.Network, Address: p.Host}
		p.pactClient = client
	}

	// Need to predefine due to scoping
	var port int
	var perr error
	if p.AllowedMockServerPorts != "" {
		port, perr = utils.FindPortInRange(p.AllowedMockServerPorts)
	} else {
		port, perr = utils.GetFreePort()
	}
	if perr != nil {
		log.Println("[ERROR] unable to find free port, mockserver will fail to start")
	}

	if p.Server == nil && startMockServer {
		log.Println("[DEBUG] starting mock service on port:", port)

		args := []string{
			"--pact-specification-version",
			fmt.Sprintf("%d", p.SpecificationVersion),
			"--pact-dir",
			filepath.FromSlash(p.PactDir),
			"--log",
			filepath.FromSlash(p.LogDir + "/" + "pact.log"),
			"--consumer",
			p.Consumer,
			"--provider",
			p.Provider,
		}

		p.Server = p.pactClient.StartServer(args, port)
	}

	return p
}

// Configure logging
func (p *Pact) setupLogging() {
	log.Print("[DEBUG] LogLevel", p.LogLevel)
	if p.logFilter == nil {
		if p.LogLevel == "" {
			p.LogLevel = "INFO"
		}
		p.logFilter = &logutils.LevelFilter{
			Levels:   []logutils.LogLevel{"DEBUG", "WARN", "ERROR"},
			MinLevel: logutils.LogLevel(p.LogLevel),
			Writer:   os.Stderr,
		}
		log.SetOutput(p.logFilter)
	}
	log.Printf("[DEBUG] pact setup logging")
}

// Teardown stops the Pact Mock Server. This usually is called on completion
// of each test suite.
func (p *Pact) Teardown() *Pact {
	log.Printf("[DEBUG] teardown")
	if p.Server != nil {
		p.Server = p.pactClient.StopServer(p.Server)
	}
	return p
}

// Verify runs the current test case against a Mock Service.
// Will cleanup interactions between tests within a suite.
func (p *Pact) Verify(integrationTest func() error) error {
	p.Setup(true)
	log.Printf("[DEBUG] pact verify")
	mockServer := &MockService{
		BaseURL:  fmt.Sprintf("http://%s:%d", p.Host, p.Server.Port),
		Consumer: p.Consumer,
		Provider: p.Provider,
	}

	for _, interaction := range p.Interactions {
		err := mockServer.AddInteraction(interaction)
		if err != nil {
			return err
		}
	}

	// Run the integration test
	err := integrationTest()
	if err != nil {
		return err
	}

	// Run Verification Process
	err = mockServer.Verify()
	if err != nil {
		return err
	}

	// Clear out interations
	p.Interactions = make([]*Interaction, 0)

	return mockServer.DeleteInteractions()
}

// WritePact should be called writes when all tests have been performed for a
// given Consumer <-> Provider pair. It will write out the Pact to the
// configured file.
func (p *Pact) WritePact() error {
	p.Setup(true)
	log.Printf("[DEBUG] pact write Pact file")
	mockServer := MockService{
		BaseURL:           fmt.Sprintf("http://%s:%d", p.Host, p.Server.Port),
		Consumer:          p.Consumer,
		Provider:          p.Provider,
		PactFileWriteMode: p.PactFileWriteMode,
	}
	err := mockServer.WritePact()
	if err != nil {
		return err
	}

	return nil
}

// VerifyProviderRaw reads the provided pact files and runs verification against
// a running Provider API, providing raw response from the Verification process.
func (p *Pact) VerifyProviderRaw(request types.VerifyRequest) (types.ProviderVerifierResponse, error) {
	p.Setup(false)

	// If we provide a Broker, we go to it to find consumers
	if request.BrokerURL != "" {
		log.Printf("[DEBUG] pact provider verification - finding all consumers from broker: %s", request.BrokerURL)
		err := findConsumers(p.Provider, &request)
		if err != nil {
			return types.ProviderVerifierResponse{}, err
		}
	}

	log.Printf("[DEBUG] pact provider verification")

	return p.pactClient.VerifyProvider(request)
}

// VerifyProvider accepts an instance of `*testing.T`
// running the provider verification with granular test reporting and
// automatic failure reporting for nice, simple tests.
func (p *Pact) VerifyProvider(t *testing.T, request types.VerifyRequest) (types.ProviderVerifierResponse, error) {
	res, err := p.VerifyProviderRaw(request)

	if err != nil {
		t.Fatal("Error:", err)
		return res, err
	}

	for _, example := range res.Examples {
		t.Run(example.Description, func(st *testing.T) {
			st.Log(example.FullDescription)
			if example.Status != "passed" {
				st.Errorf("%s\n", example.Exception.Message)
			}
		})
	}

	return res, err
}

// VerifyProducer accepts an instance of `*testing.T`
// running provider message verification with granular test reporting and
// automatic failure reporting for nice, simple tests.
func (p *Pact) VerifyProducer(t *testing.T, request types.VerifyRequest, handlers map[string]func(...interface{}) (map[string]interface{}, error)) (types.ProviderVerifierResponse, error) {

	// Starts the message wrapper API with hooks back to the message handlers
	// This maps the 'description' field of a message pact, to a function handler
	// that will implement the message producer. This function must return an object and optionally
	// and error. The object will be marshalled to JSON for comparison.
	mux := http.NewServeMux()

	// TODO: make this dynamic
	port := 9393
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		// Extract message
		var message types.Message
		body, err := ioutil.ReadAll(r.Body)
		r.Body.Close()

		if err != nil {
			// TODO: How should we respond back to the verifier in this case? 50x?
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		json.Unmarshal(body, &message)

		// Lookup key in function mapping
		f, messageFound := handlers[message.Description]

		if !messageFound {
			// TODO: How should we respond back to the verifier in this case? 50x?
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Execute function handler
		res, handlerErr := f()

		fmt.Printf("[DEBUG] f() returned: %v", res)

		if handlerErr != nil {
			// TODO: How should we respond back to the verifier in this case? 50x?
			fmt.Println("[ERROR] error handling function:", handlerErr)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		// Write the body back
		resBody, errM := json.Marshal(res)
		if errM != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Println("[ERROR] error marshalling objcet:", errM)
			return
		}
		fmt.Printf("[DEBUG] sending response body back to verifier %v", resBody)

		w.WriteHeader(http.StatusOK)
		w.Write(resBody)
	})

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	log.Printf("[DEBUG] API handler starting: port %d (%s)", port, ln.Addr())
	go http.Serve(ln, mux)

	portErr := waitForPort(port, "tcp", "localhost", fmt.Sprintf(`Timed out waiting for Daemon on port %d - are you
		sure it's running?`, port))

	if portErr != nil {
		t.Fatal("Error:", err)
		return types.ProviderVerifierResponse{}, portErr
	}

	res, err := p.VerifyProviderRaw(request)

	if err != nil {
		t.Fatal("Error:", err)
		return res, err
	}

	for _, example := range res.Examples {
		t.Run(example.Description, func(st *testing.T) {
			st.Log(example.FullDescription)
			if example.Status != "passed" {
				st.Errorf("%s\n", example.Exception.Message)
			}
		})
	}

	return res, err
}

// VerifyMessage creates a new Pact _message_ interaction to build a testable
// interaction
func (p *Pact) VerifyMessage(message *Message, handler func(...types.Message) error) (types.CommandResponse, error) {
	log.Printf("[DEBUG] pact add message")
	p.Setup(false)

	// Yield message, and send through handler function
	// TODO: for now just call the handler
	err := handler(message.message)
	if err != nil {
		return types.CommandResponse{}, err
	}

	// If no errors, update Message Pact
	res, err := p.pactClient.UpdateMessagePact(types.PactMessageRequest{
		Message:       message.message,
		Consumer:      p.Consumer,
		Provider:      p.Provider,
		PactWriteMode: p.PactFileWriteMode,
		PactDir:       p.PactDir,
	})

	return res, err
}
