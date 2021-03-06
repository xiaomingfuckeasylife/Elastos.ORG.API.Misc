package chain

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/elastos/Elastos.ELA.Utility/common"
	"github.com/elastos/Elastos.ELA.Utility/crypto"
	"github.com/elastos/Elastos.ORG.API.Misc/config"
	"github.com/elastos/Elastos.ORG.API.Misc/db"
	"github.com/elastos/Elastos.ORG.API.Misc/log"
	"io/ioutil"
	"math"
	"net/http"
	"strconv"
	"time"
)

const (
	BlockHeight       = "/api/v1/block/height"
	BlockDetail       = "/api/v1/block/details/height/"
	TransactionDetail = "/api/v1/transaction/"
	INCOME            = "income"
	SPEND             = "spend"
	ELA               = 100000000
	MINEING_ADDR      = "0000000000000000000000000000000000"
	CROSS_CHAIN_FEE   = 10000
)

var dba = db.NewInstance()

type Address_history struct {
	History  []Block_transaction_history
	TotalNum int
}

type Block_transaction_history struct {
	Txid       string
	Type       string
	Value      int64
	CreateTime int64
	Height     int
	Fee        int64
	Inputs     []string
	Outputs    []string
}

type Did_Property struct{
	Did 				string
	Did_status  		int
	Public_key  		string
	Property_key    	string
	property_key_status int
	Property_value 		string
	Txid				string
	Block_time			int
	Height 				int
}

//Sync sync chain data
func Sync() {
	for {
		tx, err := dba.Begin()
		if err = doSync(tx); err != nil {
			log.Infof("Sync Height Error : %v \n", err.Error())
			tx.Rollback()
		} else {
			tx.Commit()
		}
		<-time.After(time.Second * 10)
	}
}

//get get data from givin url and return map as value
func get(url string) (map[string]interface{}, error) {
	log.Infof("Request URL = %v \n", url)
	r, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	resp, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	rstMap := make(map[string]interface{})
	json.Unmarshal(resp, &rstMap)
	return rstMap, nil
}

func doSync(tx *sql.Tx) error {

	resp, err := get(config.Conf.Node + BlockHeight)

	if err != nil {
		return err
	}

	r, err := tx.Query("select height from chain_block_transaction_history order by id desc limit 1")
	if err != nil {
		return err
	}
	storeHeight := -1
	if r.Next() {
		r.Scan(&storeHeight)
	}
	r.Close()

	chainHeight, ok := resp["Result"]
	if ok {
		if storeHeight == int(chainHeight.(float64)) {
			return nil
		}
		count := 0
		for curr := storeHeight + 1; curr <= int(chainHeight.(float64)); curr++ {
			err = handleHeight(curr,tx)
			if err != nil {
				return err
			}
			count++
			if count%5000 == 0 {
				return nil
			}
		}
	}

	return nil
}

func handleHeight(curr int,tx *sql.Tx) error{
	resp, err := get(config.Conf.Node + BlockDetail + strconv.FormatInt(int64(curr), 10))
	if err != nil {
		return err
	}
	txArr := (resp["Result"].(map[string]interface{}))["tx"].([]interface{})
	if len(txArr) == 0 {
		return nil
	}
	for _, v := range txArr {
		stmt, err := tx.Prepare("insert into chain_block_transaction_history (address,txid,type,value,createTime,height,fee,inputs,outputs,memo) values(?,?,?,?,?,?,?,?,?,?)")
		if err != nil {
			return err
		}

		vm := v.(map[string]interface{})
		txid := vm["txid"].(string)
		time := vm["blocktime"].(float64)
		t := vm["type"].(float64)
		_type := INCOME
		attrArr := vm["attributes"].([]interface{})
		memo := ""
		if len(attrArr) != 0 {
			var ok bool
			memo , ok = attrArr[0].(map[string]interface{})["data"].(string)
			if !ok {
				log.Warn("wrong data format")
			}
			err := handleMemo(memo,curr,txid,int(time),tx)
			if err != nil {
				log.Warnf("Error parsing error memo = %v , error = %s", attrArr[0], err.Error())
			}
		}
		if t == 0 {
			vout := vm["vout"].([]interface{})
			coinbase := make([]map[string]interface{}, 0)
			to := ""
			for _, vv := range vout {
				vvm := vv.(map[string]interface{})
				value := vvm["value"].(string)
				address := vvm["address"].(string)
				coinbaseMap := make(map[string]interface{})
				fv, err := strconv.ParseFloat(value, 64)
				if err != nil {
					return err
				}
				coinbaseMap["value"] = int64(fv * ELA)
				coinbaseMap["address"] = address
				coinbase = append(coinbase, coinbaseMap)
				to += address + ","

			}

			for _, v := range coinbase {
				_, err := stmt.Exec(v["address"], txid, _type, v["value"], strconv.FormatFloat(time, 'f', 0, 64), curr, 0, MINEING_ADDR, to,"")
				if err != nil {
					return err
				}
			}

		} else {

			vin := vm["vin"].([]interface{})
			spend := make(map[string]float64)
			totalInput := 0.0
			from := ""
			to := ""
			for _, vv := range vin {
				vvm := vv.(map[string]interface{})
				vintxid := vvm["txid"].(string)
				vinindex := vvm["vout"].(float64)
				txResp, err := get(config.Conf.Node + TransactionDetail + vintxid)
				if err != nil {
					return err
				}
				vout := ((txResp["Result"].(map[string]interface{}))["vout"].([]interface{}))[int(vinindex)]
				voutm := vout.(map[string]interface{})
				address := voutm["address"].(string)
				value, err := strconv.ParseFloat(voutm["value"].(string), 64)
				totalInput += value
				if err != nil {
					return err
				}
				v, ok := spend[address]
				if ok {
					spend[address] = v + value
				} else {
					spend[address] = value
				}
				from += address + ","
			}
			vout := vm["vout"].([]interface{})
			receive := make(map[string]float64)
			totalOutput := 0.0
			for _, vv := range vout {
				vvm := vv.(map[string]interface{})
				value, err := strconv.ParseFloat(vvm["value"].(string), 64)
				totalOutput += value
				if err != nil {
					return err
				}
				address := vvm["address"].(string)
				v, ok := receive[address]
				if ok {
					receive[address] = v + value
				} else {
					receive[address] = value
				}
				to += address + ","
			}
			fee := int64(math.Round((totalInput - totalOutput) * ELA))
			if fee < 0 {
				fee = CROSS_CHAIN_FEE
			}
			for k, r := range receive {
				_type = INCOME
				s, ok := spend[k]
				var value float64
				if ok {
					if s > r {
						value = math.Round((s - r) * ELA)
						_type = SPEND
					} else {
						value = math.Round((r - s) * ELA)
					}
					delete(spend, k)
				} else {
					value = math.Round(r * ELA)
				}
				_, err := stmt.Exec(k, txid, _type, int64(value), strconv.FormatFloat(time, 'f', 0, 64), curr, fee, from, to,"")
				if err != nil {
					return err
				}
			}

			for k, r := range spend {
				_type = SPEND
				_, err := stmt.Exec(k, txid, _type, int64(r*ELA), strconv.FormatFloat(time, 'f', 0, 64), curr, fee, from, to,memo)
				if err != nil {
					return err
				}
			}
		}

		stmt.Close()
	}
	return nil
}

func handleMemo(memo string,height int , txid string , createTime int,tx *sql.Tx) error{
	b , err := hex.DecodeString(memo)
	if err != nil {
		return err
	}
	mm := make(map[string]interface{})
	err = json.Unmarshal(b,&mm)
	if err != nil {
		return errors.New("Not a valid string")
	}

	msg , ok0 := mm["msg"].(string)
	pub , ok1 := mm["pub"].(string)
	sig , ok2 := mm["sig"].(string)

	if !(ok0 && ok1 && ok2) {
		return errors.New("No 'msg' or 'pub' or 'sig' key in memo")
	}

	pubKey, err := hex.DecodeString(pub)
	if err != nil {
		return err
	}

	publicKey, err := crypto.DecodePoint(pubKey)
	if err != nil {
		return err
	}

	data, _ := hex.DecodeString(msg)
	sign, _ := hex.DecodeString(sig)
	err = crypto.Verify(*publicKey, data, sign)
	if err != nil {
		return err
	}

	raw := make(map[string]interface{})
	err = json.Unmarshal(data,&raw)
	if err != nil {
		return errors.New("RawData is not Json")
	}

	fstats , ko := raw["Status"].(float64)
	props , ko1 := raw["Properties"].([]interface{})

	if !(ko && ko1) {
		return errors.New("Can not find Status or Properties in did msg")
	}
	istats := int64(fstats)
	for _ , v := range props {
		in , ko3 := v.(map[string]interface{})

		if !ko3 {
			log.Warn("Properties is not valid ")
			continue
		}

		key , ko4 :=in["Key"].(string)
		val , ko5 :=in["Value"].(string)
		keyStats , ko6 := in["Status"].(float64)
		if !(ko4 && ko5 && ko6) {
			log.Warn("Can not find Key or Value or Status in properties")
			continue
		}

		did , _ := getDid(pub)
		if err != nil {
			log.Warn(err.Error())
			continue
		}
		stmt , err := tx.Prepare("insert into chain_did_property(did,did_status,public_key,property_key,property_key_status,property_value,txid,block_time,height) values(?,?,?,?,?,?,?,?,?)")
		if err != nil {
			log.Warn(err.Error())
			continue
		}
		_ , err = stmt.Exec(did,istats,pub,key,keyStats,val,txid,createTime,height)
		if err != nil {
			log.Warn(err)
			continue
		}
	}

	return nil
}

func getDid(pub string) (string , error){
	pubKey, err := hex.DecodeString(pub)
	if err != nil {
		return "",err
	}
	publicKey, err := crypto.DecodePoint(pubKey)
	if err != nil {
		return "",err
	}
	redeemScript , err := CreateRegistedRedeemedScript(publicKey)
	if err != nil {
		return "",err
	}
	uint168 , err := crypto.ToProgramHash(redeemScript)
	if err != nil {
		return "",err
	}
	did , err := uint168.ToAddress()
	if err != nil {
		return "",err
	}
	return did,nil
}

func CreateRegistedRedeemedScript(publicKey *crypto.PublicKey) ([]byte, error) {
	content, err := publicKey.EncodePoint(true)
	if err != nil {
		return nil, errors.New("create standard redeem script, encode public key failed")
	}
	buf := new(bytes.Buffer)
	buf.WriteByte(byte(len(content)))
	buf.Write(content)
	buf.WriteByte(byte(common.REGISTERID))

	return buf.Bytes(), nil
}