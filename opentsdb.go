package timeline

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/uol/logh"
	serializer "github.com/uol/serializer/opentsdb"
)

/**
* The OpenTSDB transport implementation.
* @author rnojiri
**/

// OpenTSDBTransport - implements the openTSDB transport
type OpenTSDBTransport struct {
	core          transportCore
	configuration *OpenTSDBTransportConfig
	serializer    *serializer.Serializer
	address       *net.TCPAddr
	connection    net.Conn
	started       bool
	connected     bool
}

// OpenTSDBTransportConfig - has all openTSDB event manager configurations
type OpenTSDBTransportConfig struct {
	DefaultTransportConfiguration
	MaxReadTimeout      time.Duration
	ReconnectionTimeout time.Duration
}

type rwOp string

const (
	read  rwOp = "read"
	write rwOp = "write"
)

// NewOpenTSDBTransport - creates a new openTSDB event manager
func NewOpenTSDBTransport(configuration *OpenTSDBTransportConfig) (*OpenTSDBTransport, error) {

	if configuration == nil {
		return nil, fmt.Errorf("null configuration found")
	}

	if err := configuration.Validate(); err != nil {
		return nil, err
	}

	if configuration.MaxReadTimeout.Seconds() <= 0 {
		return nil, fmt.Errorf("invalid connection maximum read timeout: %s", configuration.MaxReadTimeout)
	}

	if configuration.ReconnectionTimeout.Seconds() <= 0 {
		return nil, fmt.Errorf("invalid connection reconnection timeout: %s", configuration.ReconnectionTimeout)
	}

	s := serializer.New(configuration.SerializerBufferSize)

	t := &OpenTSDBTransport{
		core: transportCore{
			batchSendInterval: configuration.BatchSendInterval,
			pointChannel:      make(chan interface{}, configuration.TransportBufferSize),
			loggers:           logh.CreateContextualLogger("pkg", "timeline/opentsdb"),
		},
		configuration: configuration,
		serializer:    s,
	}

	t.core.transport = t

	return t, nil
}

// ConfigureBackend - configures the backend
func (t *OpenTSDBTransport) ConfigureBackend(backend *Backend) error {

	if backend == nil {
		return fmt.Errorf("no backend was configured")
	}

	var err error
	t.address, err = net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", backend.Host, backend.Port))
	if err != nil {
		return err
	}

	return nil
}

// DataChannel - send a new point
func (t *OpenTSDBTransport) DataChannel() chan<- interface{} {

	return t.core.pointChannel
}

// recover - recovers from panic
func (t *OpenTSDBTransport) recover() {

	if r := recover(); r != nil {
		if logh.ErrorEnabled {
			t.core.loggers.Error().Msg(fmt.Sprintf("recovered from: %s", r))
		}
	}
}

// TransferData - transfers the data to the backend throught this transport
func (t *OpenTSDBTransport) TransferData(dataList []interface{}) error {

	numPoints := len(dataList)
	points := make([]*serializer.ArrayItem, numPoints)

	var ok bool
	for i := 0; i < numPoints; i++ {
		points[i], ok = dataList[i].(*serializer.ArrayItem)
		if !ok {
			return fmt.Errorf("error casting data to serializer.ArrayItem")
		}
	}

	if logh.DebugEnabled {
		for i := 0; i < len(points); i++ {
			logh.Debug().Msgf("point: %+v", points[i])
		}
	}

	payload, err := t.serializer.SerializeArray(points...)
	if err != nil {
		return err
	}

	if logh.DebugEnabled {
		logh.Debug().Msgf("sending a payload of %d bytes", len(payload))
	}

	defer t.recover()

	for {
		if !t.writePayload(payload) {
			t.closeConnection()
			t.retryConnect()
		} else {
			break
		}
	}

	return nil
}

// writePayload - writes the payload
func (t *OpenTSDBTransport) writePayload(payload string) bool {

	if !t.connected {
		if logh.InfoEnabled {
			t.core.loggers.Info().Msg("connection is not ready...")
		}
		return false
	}

	err := t.connection.SetWriteDeadline(time.Now().Add(t.configuration.RequestTimeout))
	if err != nil {
		if logh.ErrorEnabled {
			t.core.loggers.Error().Err(err).Msg("error setting write deadline")
		}
		return false
	}

	_, err = t.connection.Write([]byte(payload))
	if err != nil {
		t.logConnectionError(err, read)
		return false
	}

	readBuffer := make([]byte, 32)

	err = t.connection.SetReadDeadline(time.Now().Add(t.configuration.MaxReadTimeout))
	if err != nil {
		if logh.ErrorEnabled {
			t.core.loggers.Error().Err(err).Msg("error setting read deadline")
		}
		return false
	}

	_, err = t.connection.Read(readBuffer)
	if err != nil {
		if castedErr, ok := err.(net.Error); ok && !castedErr.Timeout() {
			t.logConnectionError(err, read)
			return false
		}
	}

	return true
}

// logConnectionError - logs the connection error
func (t *OpenTSDBTransport) logConnectionError(err error, operation rwOp) {

	if err == io.EOF {
		if logh.ErrorEnabled {
			t.core.loggers.Error().Msg(fmt.Sprintf("[%s] connection EOF received, retrying connection...", operation))
		}

		return
	}

	if castedErr, ok := err.(net.Error); ok && castedErr.Timeout() {
		if logh.ErrorEnabled {
			t.core.loggers.Error().Msg(fmt.Sprintf("[%s] connection timeout received, retrying connection...", operation))
		}

		return
	}

	if logh.ErrorEnabled {
		t.core.loggers.Error().Msg(fmt.Sprintf("[%s] error executing operation on connection: %s", operation, err.Error()))
	}
}

// closeConnection - closes the active connection
func (t *OpenTSDBTransport) closeConnection() {

	err := t.connection.Close()
	if err != nil {
		if logh.ErrorEnabled {
			t.core.loggers.Error().Msg(err.Error())
		}
	}

	if logh.InfoEnabled {
		t.core.loggers.Info().Msg("connection closed")
	}

	t.connection = nil
	t.connected = false
}

// MatchType - checks if this transport implementation matches the given type
func (t *OpenTSDBTransport) MatchType(tt transportType) bool {

	return tt == typeOpenTSDB
}

// retryConnect - connects the telnet client
func (t *OpenTSDBTransport) retryConnect() {

	if logh.InfoEnabled {
		t.core.loggers.Info().Msgf("starting a new connection to: %s:", t.address.String())
	}

	for {
		t.connect()
		if t.connected {
			break
		}

		<-time.After(t.configuration.ReconnectionTimeout)
	}

	if logh.InfoEnabled {
		t.core.loggers.Info().Msg("connected!")
	}
}

// connect - connects the telnet client
func (t *OpenTSDBTransport) connect() {

	if logh.InfoEnabled {
		t.core.loggers.Info().Msg(fmt.Sprintf("connecting to opentsdb telnet: %s:", t.address.String()))
	}

	t.connected = false

	var err error
	t.connection, err = net.DialTCP("tcp", nil, t.address)
	if err != nil {
		if logh.ErrorEnabled {
			t.core.loggers.Info().Msg(fmt.Sprintf("error connecting to address: %s", t.address.String()))
		}
		return
	}

	err = t.connection.SetDeadline(time.Time{})
	if err != nil {
		if logh.ErrorEnabled {
			t.core.loggers.Error().Msg("error setting connection's deadline")
		}
		return
	}

	t.connected = true
}

// Start - starts this transport
func (t *OpenTSDBTransport) Start() error {

	t.retryConnect()

	return t.core.Start()
}

// Close - closes this transport
func (t *OpenTSDBTransport) Close() {

	t.core.Close()

	t.connected = false
}

// Serialize - renders the text using the configured serializer
func (t *OpenTSDBTransport) Serialize(item interface{}) (string, error) {

	return t.serializer.SerializeGeneric(item)
}
