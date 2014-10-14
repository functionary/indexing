package main

import "flag"
import "fmt"
import "log"
import "os"
import "strconv"
import "strings"
import "time"

import "github.com/couchbase/indexing/secondary/common"
import "github.com/couchbase/indexing/secondary/dataport"
import "github.com/couchbase/indexing/secondary/projector"
import "github.com/couchbase/indexing/secondary/protobuf"

var pooln = "default"

var options struct {
	endpoints     []string // list of endpoint daemon to start
	coordEndpoint string   // co-ordinator endpoint
	stat          string   // periodic timeout to print dataport statistics
	timeout       string   // timeout to loop
	maxVbno       int      // maximum number of vbuckets
	addBuckets    []string
	delBuckets    []string
	loop          int
	debug         bool
	trace         bool
}

func argParse() string {
	addBuckets := "default"
	delBuckets := "default"
	endpoints := "localhost:9020"

	flag.StringVar(&endpoints, "endpoints", endpoints,
		"list of endpoint daemon to start")
	flag.StringVar(&options.coordEndpoint, "coorendp", "localhost:9021",
		"co-ordinator endpoint")
	flag.StringVar(&options.stat, "stat", "1000",
		"periodic timeout to print dataport statistics")
	flag.StringVar(&options.timeout, "timeout", "0",
		"timeout to loop")
	flag.IntVar(&options.maxVbno, "maxvb", 1024,
		"maximum number of vbuckets")
	flag.StringVar(&addBuckets, "addBuckets", addBuckets,
		"buckets to add")
	flag.StringVar(&delBuckets, "delBuckets", addBuckets,
		"buckets to del")
	flag.IntVar(&options.loop, "loop", 10,
		"repeat bucket-add and bucket-del loop number of times")
	flag.BoolVar(&options.debug, "debug", false,
		"run in debug mode")
	flag.BoolVar(&options.trace, "trace", false,
		"run in trace mode")

	flag.Parse()

	options.addBuckets = strings.Split(addBuckets, ",")
	options.delBuckets = strings.Split(delBuckets, ",")
	options.endpoints = strings.Split(endpoints, ",")
	if options.debug {
		common.SetLogLevel(common.LogLevelDebug)
	} else if options.trace {
		common.SetLogLevel(common.LogLevelTrace)
	} else {
		common.SetLogLevel(common.LogLevelInfo)
	}

	args := flag.Args()
	if len(args) < 1 {
		usage()
		os.Exit(1)
	}
	return args[0]
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage : %s [OPTIONS] <addr> \n", os.Args[0])
	flag.PrintDefaults()
}

var projectors = make(map[string]*projector.Client)

func main() {
	cluster := argParse()

	// start dataport servers.
	for _, endpoint := range options.endpoints {
		stat, _ := strconv.Atoi(options.stat)
		go dataport.Application(
			endpoint, stat, 0,
			func(addr string, msg interface{}) bool { return true })
	}
	go dataport.Application(options.coordEndpoint, 0, 0, nil)

	timeout, err := strconv.Atoi(options.timeout)
	if err != nil {
		log.Fatal(err)
	}

	kvaddrs, err := common.GetKVAddrs(cluster, pooln, "default" /*bucket*/)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("found %v nodes\n", kvaddrs)

	// spawn initial set of projectors
	for _, kvaddr := range kvaddrs {
		adminport := kvaddr2adminport(kvaddr, 500)
		settings := map[string]interface{}{
			"cluster":   cluster,
			"adminport": adminport,
			"kvaddrs":   []string{kvaddr},
			"epfactory": common.RouterEndpointFactory(EndpointFactory),
		}
		projector.NewProjector(settings) // start projector daemon
		projectors[kvaddr] = projector.NewClient(adminport)
	}

	// index instances for initial bucket []string{default}.
	instances := protobuf.ExampleIndexInstances(
		[]string{"beer-sample"}, options.endpoints, options.coordEndpoint)

	endpointSettings := map[string]interface{}{
		"type": "dataport",
	}

	// start backfill stream on each projector
	for kvaddr, client := range projectors {
		// start backfill stream on each projector
		_, err := client.InitialTopicRequest(
			"backfill", "default", kvaddr, endpointSettings, instances)
		if err != nil {
			log.Fatal(err)
		}
	}

loop:
	if options.addBuckets != nil {
		// add `buckets` and its instances after few seconds
		<-time.After(time.Duration(timeout) * time.Millisecond)
		instances = protobuf.ExampleIndexInstances(
			options.addBuckets, options.endpoints, options.coordEndpoint)
		for kvaddr, client := range projectors {
			ts, err := client.InitialRestartTimestamp(
				pooln, "default", []string{kvaddr})
			if err != nil {
				log.Fatal(err)
			}
			reqTss := []*protobuf.TsVbuuid{ts}
			res, err := client.AddBuckets("backfill", reqTss, instances)
			if err != nil {
				log.Fatal(err)
			}
			if err := res.GetErr(); err != nil {
				log.Fatal(err)
			}
		}
	}

	if options.delBuckets != nil {
		// del `buckets` and its instances after few seconds
		<-time.After(time.Duration(timeout) * time.Millisecond)
		for _, client := range projectors {
			err := client.DelBuckets("backfill", options.delBuckets)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	options.loop--
	if options.loop > 0 {
		goto loop
	}

	<-make(chan bool) // wait for ever
}

func mf(err error, msg string) {
	if err != nil {
		log.Fatalf("%v: %v", msg, err)
	}
}

func kvaddr2adminport(kvaddr string, offset int) string {
	ss := strings.Split(kvaddr, ":")
	kport, err := strconv.Atoi(ss[1])
	if err != nil {
		log.Fatal(err)
	}
	return ss[0] + ":" + strconv.Itoa(kport+offset)
}

// EndpointFactory to create endpoint instances based on settings.
func EndpointFactory(
	topic, addr string,
	settings map[string]interface{}) (common.RouterEndpoint, error) {

	switch v := settings["type"].(string); v {
	case "dataport":
		return dataport.NewRouterEndpoint(topic, addr, settings)
	default:
		log.Fatal("Unknown endpoint type")
	}
	return nil, nil
}
