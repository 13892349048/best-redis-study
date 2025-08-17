package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

func mainPubSub() {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	pubsub := rdb.PSubscribe(ctx, "news*", "orders.*")
	defer pubsub.Close()

	if _, err := pubsub.Receive(ctx); err != nil {
		log.Fatal(err)
	}
	msgCh := pubsub.Channel()
	go func() {
		for msg := range msgCh {
			fmt.Printf("channel: %q, pattern: %q, message: %q\n", msg.Channel, msg.Pattern, msg.Payload)
		}
	}()

	// 发布消息 确保订阅好了
	time.Sleep(time.Second * 1)

	type kv struct {
		ch, msg string
	}

	kvPub := []kv{
		{"news.tech", "Redis 6.0 发布"},
		{"news.life", "Redis 7.0 发布"},
		{"news.life", "Redis 8.0 发布"},
		{"news.life", "Redis 9.0 发布"},
		{"news.life", "Redis 10.0 发布"},
	}

	for _, v := range kvPub {
		if _, err := rdb.Publish(ctx, v.ch, v.msg).Result(); err != nil {
			log.Println(err)
		} else {
			fmt.Printf("publish %q to %q\n", v.msg, v.ch)
		}
	}
	time.Sleep(time.Second * 5)
	fmt.Println("done")
}
