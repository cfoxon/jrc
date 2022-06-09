package jrc

import (
	"bytes"
	"errors"
	"github.com/goccy/go-json"
	"github.com/valyala/fasthttp"
	"net/url"
	"sync"
)

type RPCRequests []*RpcRequest

//RpcRequest contains a JSON RPC 2.0 request to be submitted to a Server
type RpcRequest struct {
	JsonRpc string      `json:"jsonrpc"`
	Id      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

//RpcResponse contains an RPC response with the Result field left un-decoded
type RpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RpcError       `json:"error,omitempty"`
	ID      int             `json:"id"`
}

//RpcError holds decoded RPC errors
type RpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

//Server contains information related to connecting to an RPC server
type Server struct {
	url   *url.URL
	hc    *fasthttp.HostClient
	conn  int
	batch int
	reqc  chan *fasthttp.Request
	resc  chan []byte
	wg    *sync.WaitGroup
}

//SetOption changes server configuration with options
func (srv *Server) SetOption(options ...func(*Server) error) error {
	for _, opt := range options {
		if err := opt(srv); err != nil {
			return err
		}
	}
	return nil
}

func (srv *Server) setAddress(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return err
	}
	srv.url = u
	return nil
}

func (srv *Server) setMaxCon(n int) error {
	srv.conn = n
	return nil
}

func (srv *Server) setMaxBatch(n int) error {
	srv.batch = n
	return nil
}

//ExecBatch executes a batch of calls and parses the JSON RPC 2.0 portion of the body
//  the Result field is left as json.RawMessage for further parsing by the caller
func (srv *Server) ExecBatch(rs RPCRequests) ([]RpcResponse, error) {
	bs, err := srv.ExecBatchFast(rs)
	if err != nil {
		return nil, err
	}
	resps, err := parseBatch(bs)
	if err != nil {
		return nil, err
	}
	return resps, nil
}

//Exec executes a single remote procedure call
func (srv *Server) Exec(r RpcRequest) (*RpcResponse, error) {
	resps, err := srv.ExecBatch(RPCRequests{&r})
	if err != nil {
		return nil, err
	}
	return &resps[0], nil
}

//startClients starts n background tasks to make requests to the Server
func (srv *Server) startClients(n int) {
	for i := 0; i < n; i++ {
		go srv.client()
	}
}

//ExecBatchFast returns a slice of []byte containing the responses to the remote procedure calls
func (srv *Server) ExecBatchFast(rs RPCRequests) ([][]byte, error) {
	if rs == nil || len(rs) < 1 {
		return nil, nil
	}
	maxConn := srv.conn
	if maxConn > len(rs) {
		maxConn = len(rs)
	}
	srv.startClients(maxConn)
	rc := make(chan [][]byte)
	go srv.responses(rc)

	uri := fasthttp.AcquireURI()

	if err := uri.Parse(nil, []byte(srv.url.String())); err != nil {
		return nil, err
	}
	var batch RPCRequests
	for _, rrc := range rs {
		batch = append(batch, rrc)
		if len(batch) == srv.batch {
			req := fasthttp.AcquireRequest()
			req.SetURI(uri)
			addDefaultHeaders(req)

			b, _ := json.Marshal(batch)
			req.SetBodyRaw(b)

			srv.wg.Add(1)

			srv.reqc <- req
			batch = nil
		}
	}
	if len(batch) > 0 {
		req := fasthttp.AcquireRequest()
		req.SetURI(uri)
		addDefaultHeaders(req)
		b, _ := json.Marshal(batch)
		req.SetBodyRaw(b)
		srv.wg.Add(1)
		srv.reqc <- req
	}
	fasthttp.ReleaseURI(uri)
	srv.wg.Wait()
	close(srv.resc)
	res := <-rc
	return res, nil
}

//client creates a background worker process which will monitor the requests channel for requests to make to the Server
func (srv *Server) client() {
	for {
		req := <-srv.reqc
		resp := fasthttp.AcquireResponse()
		err := srv.hc.Do(req, resp)
		fasthttp.ReleaseRequest(req)
		if err != nil {
			srv.resc <- []byte(err.Error())
			srv.wg.Done()
		} else {
			var b []byte
			contentEncoding := resp.Header.Peek("Content-Encoding")
			if bytes.EqualFold(contentEncoding, []byte("gzip")) {
				b, _ = resp.BodyGunzip()
			} else {
				b = make([]byte, len(resp.Body()))
				copy(b, resp.Body())
			}
			srv.wg.Done()
			if b != nil {
				srv.resc <- b
			}
		}
		fasthttp.ReleaseResponse(resp)
	}
}

//responses opens a channel to gather the incoming responses from the server
func (srv *Server) responses(rc chan [][]byte) {
	var bs [][]byte
	for b := range srv.resc {
		bs = append(bs, b)
	}
	rc <- bs
}

//Address sets the url of the Server
func Address(s string) func(server *Server) error {
	return func(srv *Server) error {
		return srv.setAddress(s)
	}
}

//MaxCon sets the maximum number of simultaneous connections
func MaxCon(n int) func(server *Server) error {
	return func(srv *Server) error {
		return srv.setMaxCon(n)
	}
}

//MaxBatch sets the maximum number of RPCs to batch into a single call
func MaxBatch(n int) func(server *Server) error {
	return func(srv *Server) error {
		return srv.setMaxBatch(n)
	}
}

//NewServer creates a target for clients
func NewServer(addr string, options ...func(*Server) error) (*Server, error) {
	srv, err := newDefaultServer(addr)
	if err != nil {
		return nil, err
	}
	if err = srv.SetOption(options...); err != nil {
		return nil, err
	}
	return srv, nil
}

func newDefaultServer(addr string) (*Server, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	hc := &fasthttp.HostClient{Addr: u.Host}
	if u.Scheme == "https" {
		hc.IsTLS = true
	}
	var wg sync.WaitGroup
	return &Server{
		url:   u,
		hc:    hc,
		conn:  4,
		batch: 50,
		reqc:  make(chan *fasthttp.Request),
		resc:  make(chan []byte),
		wg:    &wg,
	}, nil
}

func parseBatch(bs [][]byte) ([]RpcResponse, error) {
	var resps []RpcResponse
	for _, b := range bs {
		var r []RpcResponse
		if err := json.Unmarshal(b, &r); err != nil {
			errb := errors.New(err.Error() + "\n" + string(b))
			return nil, errb
		}
		resps = append(resps, r...)
	}
	return resps, nil
}

func addDefaultHeaders(req *fasthttp.Request) {
	req.Header.SetMethod(fasthttp.MethodPost)
	req.Header.SetContentType("application/json")
	req.Header.Set("Accept-Encoding", "gzip")
}
