// Package rabbitmq is the at-least-once dispatch work queue: quorum queue, publisher confirms, DLX, ack-after-work.
// The broker's consumer_timeout must exceed the longest task runtime.
//
// Code adapted from watermill-amqp v2.1.3 (https://github.com/ThreeDotsLabs/watermill-amqp)
// Copyright (c) 2019 Three Dots Labs
// Licensed under the MIT License
package rabbitmq
