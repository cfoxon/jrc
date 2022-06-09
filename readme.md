# JRC: Json Rpc Client
### A JSON RPC 2.0 client focused on blockchain clients
This project was primarily created to support Hive and Hive Engine projects, but should work fine as a basic client for any JSON RPC 2.0 server

## Overview
1. Instantiate server
2. Assemble RpcRequest(s)
3. Execute the request(s)
4. Parse the return

One of the project goals is to allow for optimal speeds when handling large amounts of return data, so minimal parsing is done by this client. If the caller so chooses, the unparsed RPC response ([][]byte) can be returned with `Server.ExecBatchFast`
### Instantiate a server to query
With default options:

`r, _ := jrc.NewServer("https://api.hive-engine.com")`


With custom max connections:

`r, _ := jrc.NewServer("https://api.hive-engine.com", jrc.MaxCon(10))`


Modify existing server options:

`r.SetOption(jrc.MaxBatch(1000), jrc.MaxCon(10))`


### Creating requests
Params will be marshalled to JSON

A single request:

`q := jrc.RpcRequest{Method: query.method, JsonRpc: "2.0", Id: 1, Params: query}`


Many requests (jrc will batch according to `MaxBatch`):
```
    var qs jrc.RPCRequests
    for i, query := range queries {
        q := &jrc.RpcRequest{Method: query.method, JsonRpc: "2.0", Id: i, Params: query}
        qs = append(qs, q)
    }
```

### Executing requests
A single request:

`resp, _ := r.Exec(q)`


Multiple requests:

`resps, _ := r.ExecBatch(qs)`


Multiple requests, no parsing (returns [][]byte):

`resps, _ := r.ExecBatch(qs)`

### Putting it all together

```
import "github.com/cfoxon/jrc"

func main() {
    //create a query
    query := struct {
        Contract string   `json:"contract"`
        Table    string   `json:"table"`
        Query    struct{} `json:"query"`
    }{
        Contract: "market",
        Table:    "metrics",
    }
    
    //instantiate a server to  be queried
    srv, _ := jrc.NewServer("https://ha.herpc.dtools.dev/contracts")
    
    //put the query in a request
    r := jrc.RpcRequest{JsonRpc: "2.0", Id: 1, Method: "find", Params: query}
    
    //execute the request and get the return value
    resp, _ := srv.Exec(r)
    
    //do something with the returned data
    print(string(resp.Result))
}
```