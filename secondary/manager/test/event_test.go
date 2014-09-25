// Copyright (c) 2014 Couchbase, Inc.
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
// except in compliance with the License. You may obtain a copy of the License at
//   http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the
// License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
// either express or implied. See the License for the specific language governing permissions
// and limitations under the License.

package test

import (
	"github.com/couchbase/indexing/secondary/common"
	"github.com/couchbase/indexing/secondary/manager"
	"testing"
	"time"
	"log"
)

func TestEventMgr(t *testing.T) {

	var addr = "localhost:9885"
	var leader = "localhost:9884"
	var config = "./config.json"

	log.Printf("Start Index Manager")
	mgr, err := manager.NewIndexManager(addr, leader, config)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	cleanupEvtMgrTest(mgr, t)
	
	log.Printf("Start Listening to event")	
	notifications, err := mgr.StartListenIndexCreate("TestEventMgr")
	if err != nil {
		t.Fatal(err)
	}

	// Add a new index definition : 300
	idxDefn := &common.IndexDefn{
		DefnId:          common.IndexDefnId(300),
		Name:            "event_mgr_test",
		Using:           common.ForestDB,
		Bucket:          "Default",
		IsPrimary:       false,
		OnExprList:      []string{"Testing"},
		ExprType:        common.N1QL,
		PartitionScheme: common.HASH,
		PartitionKey:    "Testing"}
	
	time.Sleep(time.Duration(1000) * time.Millisecond)
	
	log.Printf("Before DDL")	
	err = mgr.HandleCreateIndexDDL(idxDefn)
	if err != nil {
		t.Fatal(err)
	}
	
	data := listen(notifications)
	if data == nil {
		t.Fatal("Does not receive notification from watcher")
	}
	
	idxDefn, err = manager.UnmarshallIndexDefn(([]byte)(data)) 
	if err != nil {
		t.Fatal(err)
	}

	if idxDefn == nil {
		t.Fatal("Cannot unmarshall index definition")
	}
	
	if idxDefn.Name != "event_mgr_test" {
		t.Fatal("Index Definition Name mismatch")
	}

	cleanupEvtMgrTest(mgr, t)
	mgr.Close()

	time.Sleep(time.Duration(1000) * time.Millisecond)
}

// clean up
func cleanupEvtMgrTest(mgr *manager.IndexManager, t *testing.T) {

	err := mgr.HandleDeleteIndexDDL("event_mgr_test")
	if err != nil {
		t.Fatal(err)
	}
}

// listen to an event
func listen(notifications <- chan interface{}) []byte {

	timer := time.After(time.Duration(20000) * time.Millisecond)
	select {
		case data, ok := <- notifications :
			if !ok {
				return nil
			}
			
			return data.([]byte)
		case <- timer :
			return nil
	}		
	
	return nil
} 