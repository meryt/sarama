package kafka

import (
	"sort"
	"sync"
)

type metadataCache struct {
	client  *Client
	brokers map[int32]*Broker          // maps broker ids to brokers
	leaders map[string]map[int32]int32 // maps topics to partition ids to broker ids
	lock    sync.RWMutex               // protects access to the maps, only one since they're always accessed together
}

func newMetadataCache(client *Client, host string, port int32) (*metadataCache, error) {
	mc := new(metadataCache)

	starter, err := NewBroker(host, port)
	if err != nil {
		return nil, err
	}

	mc.client = client
	mc.brokers = make(map[int32]*Broker)
	mc.leaders = make(map[string]map[int32]int32)

	mc.brokers[starter.id] = starter

	// do an initial fetch of all cluster metadata by specifing an empty list of topics
	err = mc.refreshTopics(make([]*string, 0))
	if err != nil {
		return nil, err
	}

	return mc, nil
}

func (mc *metadataCache) leader(topic string, partition_id int32) *Broker {
	mc.lock.RLock()
	defer mc.lock.RUnlock()

	partitions := mc.leaders[topic]
	if partitions != nil {
		leader := partitions[partition_id]
		if leader == -1 {
			return nil
		} else {
			return mc.brokers[leader]
		}
	}

	return nil
}

func (mc *metadataCache) any() *Broker {
	mc.lock.RLock()
	defer mc.lock.RUnlock()

	for _, broker := range mc.brokers {
		return broker
	}

	return nil
}

func (mc *metadataCache) partitions(topic string) []int32 {
	mc.lock.RLock()
	defer mc.lock.RUnlock()

	partitions := mc.leaders[topic]
	if partitions == nil {
		return nil
	}

	ret := make([]int32, len(partitions))
	for id, _ := range partitions {
		ret = append(ret, id)
	}

	sort.Sort(int32Slice(ret))
	return ret
}

func (mc *metadataCache) refreshTopics(topics []*string) error {
	broker := mc.any()
	if broker == nil {
		return OutOfBrokers{}
	}

	decoder, err := broker.Send(mc.client.id, &MetadataRequest{topics})
	if err != nil {
		return err
	}
	response := decoder.(*MetadataResponse)

	mc.lock.Lock()
	defer mc.lock.Unlock()

	for i := range response.Brokers {
		broker := &response.Brokers[i]
		err = broker.Connect()
		if err != nil {
			return err
		}
		mc.brokers[broker.id] = broker
	}

	for i := range response.Topics {
		topic := &response.Topics[i]
		if topic.Err != NO_ERROR {
			return topic.Err
		}
		mc.leaders[*topic.Name] = make(map[int32]int32, len(topic.Partitions))
		for j := range topic.Partitions {
			partition := &topic.Partitions[j]
			if partition.Err != NO_ERROR {
				return partition.Err
			}
			mc.leaders[*topic.Name][partition.Id] = partition.Leader
		}
	}

	return nil
}

func (mc *metadataCache) refreshTopic(topic string) error {
	tmp := make([]*string, 1)
	tmp[0] = &topic
	return mc.refreshTopics(tmp)
}
