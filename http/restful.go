package http

import (
	"encoding/json"
	"github.com/elastos/Elastos.ORG.API.Misc/chain"
	"github.com/elastos/Elastos.ORG.API.Misc/config"
	"github.com/elastos/Elastos.ORG.API.Misc/db"
	"github.com/elastos/Elastos.ORG.API.Misc/tools"
	"github.com/gorilla/mux"
	"net/http"
	"strconv"
	"strings"
)

const ERROR_REQUEST  = "Error Request :"

var (
	// path|method|handler
	routers = map[string]map[string]http.HandlerFunc{
		"/api/1/history/{addr}":{
			"GET":history,
		},
		"/api/1/did/{did}/{key}":{
			"GET":searchKey,
		},
	}
	router = mux.NewRouter()
	dba = db.NewInstance()
)

func StartServer(){
	http.ListenAndServe(":"+config.Conf.ServerPort, router)
}

func init(){
	for p ,r := range routers {
		for m,h :=range r {
			router.HandleFunc(p,h).Methods(m)
		}
	}
}

//searchKey search did property key
func searchKey(w http.ResponseWriter,r *http.Request){
	params := mux.Vars(r)
	did := params["did"]
	key := params["key"]

	c , err := dba.ToInt("select count(*) from chain_did_property where (did_status = 0 or property_key_status = 0) and did ='" + did +"' and property_key = '" + key +"'")
	if err != nil {
		w.Write([]byte(`{"result":"` + err.Error() + `","status":500}`))
		return
	}
	if c > 0 {
		w.Write([]byte(`{"result":"did is discarded or property key is discarded","status":200}`))
		return
	}
	v , err := dba.ToStruct("select Did,Did_status,Public_key,Property_key,property_value,txid,block_time,height from chain_did_property where did ='" + did +"' and property_key = '" + key +"' and did_status = 1 and property_key_status = 1 limit 1",chain.Did_Property{})
	if err != nil {
		w.Write([]byte(`{"result":"` + err.Error() + `","status":500}`))
		return
	}
	b , err := json.Marshal(v)
	if err != nil {
		w.Write([]byte(`{"result":"` + err.Error() + `","status":500}`))
		return
	}
	w.Write([]byte(`{"result":` + string(b) + `,"status":200}`))
}

//history the address transaction history
func history(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	address := params["addr"]
	pageNum := r.FormValue("pageNum")
	var sql string
	if pageNum != "" {
		pageSize := r.FormValue("pageSize")
		var size int64
		if pageSize != "" {
			var err error
			size, err = strconv.ParseInt(pageSize, 10, 64)
			if err != nil {
				w.Write([]byte(`{"result":"` + err.Error() + `","status":400}`))
				return
			}
		} else {
			size = 10
		}
		num, err := strconv.ParseInt(pageNum, 10, 64)
		if err != nil {
			w.Write([]byte(`{"result":"` + err.Error() + `","status":400}`))
			return
		}
		from := num * (size - 1)
		sql = "select txid,type,value,createTime,height,inputs,outputs,fee from chain_block_transaction_history where address = '"+address+"' limit " + strconv.FormatInt(from, 10) + "," + strconv.FormatInt(size, 10)
	} else {
		sql = "select txid,type,value,createTime,height,inputs,outputs,fee from chain_block_transaction_history where address = '"+address+"'"
	}
	l, err := dba.Query(sql)
	if err != nil {
		w.Write([]byte(`{"result":"` + ERROR_REQUEST + err.Error() + `","status":500}`))
		return
	}
	bhs := make([]chain.Block_transaction_history, 0)
	totalNum := 0
	for e := l.Front(); e != nil ; e = e.Next(){
		history := new(chain.Block_transaction_history)
		line := e.Value.(map[string]interface{})
		tools.Map2Struct(line,history)
		inputsArr := strings.Split(line["inputs"].(string), ",")
		history.Inputs = inputsArr[:len(inputsArr)-1]
		outputsArr := strings.Split(line["outputs"].(string), ",")
		history.Outputs = outputsArr[:len(outputsArr)-1]
		if err != nil {
			w.Write([]byte(`{"result":"` + ERROR_REQUEST + err.Error() + `","status":500}`))
			return
		}
		bhs = append(bhs, *history)
	}
	l, err = dba.Query("select count(*) as count from chain_block_transaction_history where address = '"+address+"'")
	if err != nil {
		w.Write([]byte(`{"result":"` + ERROR_REQUEST + err.Error() + `","status":500}`))
		return
	}
	totalNum , _ = strconv.Atoi(l.Front().Value.(map[string]interface{})["count"].(string))
	addrHis := chain.Address_history{bhs, totalNum}
	buf, err := json.Marshal(&addrHis)
	if err != nil {
		w.Write([]byte(`{"result":"` + ERROR_REQUEST + err.Error() + `","status":500}`))
		return
	}
	w.Write([]byte(`{"result":` + string(buf) + `,"status":200}`))
}