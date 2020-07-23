// Copyright 2012 Gary Burd
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package redis

import (
	"errors"
	"time"
)

// Error represents an error returned in a command reply.
type Error string

func (err Error) Error() string { return string(err) }

// Conn represents a connection to a Redis server.
type Conn interface {
	// Close closes the connection.
	Close() error

	// Err returns a non-nil value when the connection is not usable.
	Err() error

	// Do sends a command to the server and returns the received reply.
	Do(commandName string, args ...interface{}) (reply interface{}, err error)

	// Send writes the command to the client's output buffer.
	Send(commandName string, args ...interface{}) error

	// Flush flushes the output buffer to the Redis server.
	Flush() error

	// Receive receives a single reply from the Redis server
	Receive() (reply interface{}, err error)
}

// Argument is the interface implemented by an object which wants to control how
// the object is converted to Redis bulk strings.
/*
参数是由希望控制如何将对象转换为Redis批量字符串的对象实现的接口。
*/
type Argument interface {
	// RedisArg returns a value to be encoded as a bulk string per the
	// conversions listed in the section 'Executing Commands'.
	// Implementations should typically return a []byte or string.
	/*
	根据“执行命令”一节中列出的转换，RedisArg返回一个要编码为批量字符串的值。
	实现通常应返回[]字节或字符串。
	*/
	RedisArg() interface{}
}

// Scanner is implemented by an object which wants to control its value is
// interpreted when read from Redis.
/*
scanner是由一个对象实现的，该对象希望在从Redis读取时控制其值被解释。
*/
type Scanner interface {
	// RedisScan assigns a value from a Redis value. The argument src is one of
	// the reply types listed in the section `Executing Commands`.
	//
	// An error should be returned if the value cannot be stored without
	// loss of information.
	/*
	RedisScan从Redis值中赋值。
	参数src是`执行命令`一节中列出的回复类型之一。
	如果无法在不丢失信息的情况下存储值，则应返回错误。
	*/
	RedisScan(src interface{}) error
}

// ConnWithTimeout is an optional interface that allows the caller to override
// a connection's default read timeout. This interface is useful for executing
// the BLPOP, BRPOP, BRPOPLPUSH, XREAD and other commands that block at the
// server.
//
// A connection's default read timeout is set with the DialReadTimeout dial
// option. Applications should rely on the default timeout for commands that do
// not block at the server.
//
// All of the Conn implementations in this package satisfy the ConnWithTimeout
// interface.
//
// Use the DoWithTimeout and ReceiveWithTimeout helper functions to simplify
// use of this interface.
/*
ConnWithTimeout是一个可选接口，允许调用方覆盖连接的默认读取超时。
此接口对于执行BLPOP、BRPOP、BRPOPLPUSH、XREAD和其他在服务器上阻塞的命令非常有用。
连接的默认读取超时是使用DialReadTimeout拨号选项设置的。
对于不会在服务器上阻塞的命令，应用程序应该依赖默认超时。
此包中的所有Conn实现都满足ConnWithTimeout接口。
使用DoWithTimeout和ReceiveWithTimeout帮助器函数可以简化此接口的使用。
*/
type ConnWithTimeout interface {
	Conn

	// Do sends a command to the server and returns the received reply.
	// The timeout overrides the read timeout set when dialing the
	// connection.
	/*
	DO向服务器发送命令并返回收到的回复。
	超时覆盖拨号连接时设置的读取超时。
	*/
	DoWithTimeout(timeout time.Duration, commandName string, args ...interface{}) (reply interface{}, err error)

	// Receive receives a single reply from the Redis server. The timeout
	// overrides the read timeout set when dialing the connection.
	/*
	Receive从Redis服务器接收单个回复。
	超时覆盖拨号连接时设置的读取超时。
	*/
	ReceiveWithTimeout(timeout time.Duration) (reply interface{}, err error)
}

var errTimeoutNotSupported = errors.New("redis: connection does not support ConnWithTimeout")

// DoWithTimeout executes a Redis command with the specified read timeout. If
// the connection does not satisfy the ConnWithTimeout interface, then an error
// is returned.
/*
DoWithTimeout使用指定的读取超时执行Redis命令。
如果连接不满足ConnWithTimeout接口，则返回错误。
*/
func DoWithTimeout(c Conn, timeout time.Duration, cmd string, args ...interface{}) (interface{}, error) {
	cwt, ok := c.(ConnWithTimeout)
	if !ok {
		return nil, errTimeoutNotSupported
	}
	return cwt.DoWithTimeout(timeout, cmd, args...)
}

// ReceiveWithTimeout receives a reply with the specified read timeout. If the
// connection does not satisfy the ConnWithTimeout interface, then an error is
// returned.
/*
ReceiveWithTimeout接收具有指定读取超时的回复。
如果连接不满足ConnWithTimeout接口，则返回错误。
*/
func ReceiveWithTimeout(c Conn, timeout time.Duration) (interface{}, error) {
	cwt, ok := c.(ConnWithTimeout)
	if !ok {
		return nil, errTimeoutNotSupported
	}
	return cwt.ReceiveWithTimeout(timeout)
}

// SlowLog represents a redis SlowLog
/*
SlowLog表示Redis SlowLog
*/
type SlowLog struct {
	// ID is a unique progressive identifier for every slow log entry.
	/*
	ID是每个慢速日志条目的唯一累进标识符。
	*/
	ID int64

	// Time is the unix timestamp at which the logged command was processed.
	Time time.Time

	// ExecutationTime is the amount of time needed for the command execution.
	ExecutionTime time.Duration

	// Args is the command name and arguments
	Args []string

	// ClientAddr is the client IP address (4.0 only).
	ClientAddr string

	// ClientName is the name set via the CLIENT SETNAME command (4.0 only).
	ClientName string
}
