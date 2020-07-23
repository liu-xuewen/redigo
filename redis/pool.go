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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var (
	_ ConnWithTimeout = (*activeConn)(nil)
	_ ConnWithTimeout = (*errorConn)(nil)
)

var nowFunc = time.Now // for testing

// ErrPoolExhausted is returned from a pool connection method (Do, Send,
// Receive, Flush, Err) when the maximum number of database connections in the
// pool has been reached.
/*
当达到池中的最大数据库连接数时，将从池连接方法(DO、SEND、RECEIVE、FLUSH、ERR)返回ErrPoolExhausted。
*/
var ErrPoolExhausted = errors.New("redigo: connection pool exhausted")

var (
	errPoolClosed = errors.New("redigo: connection pool closed")
	errConnClosed = errors.New("redigo: connection closed")
)

// Pool maintains a pool of connections. The application calls the Get method
// to get a connection from the pool and the connection's Close method to
// return the connection's resources to the pool.
//
// The following example shows how to use a pool in a web application. The
// application creates a pool at application startup and makes it available to
// request handlers using a package level variable. The pool configuration used
// here is an example, not a recommendation.

//Pool维护一个连接池。
//应用程序调用GET方法从池中获取连接，并调用连接的Close方法将连接的资源返回到池。
//以下示例显示如何在Web应用程序中使用池。
//应用程序在应用程序启动时创建一个池，并使其可用于使用包级变量的请求处理程序。
//此处使用的池配置是示例，而不是建议。

//  func newPool(addr string) *redis.Pool {
//    return &redis.Pool{
//      MaxIdle: 3,
//      IdleTimeout: 240 * time.Second,
//      // Dial or DialContext must be set. When both are set, DialContext takes precedence over Dial.
//      Dial: func () (redis.Conn, error) { return redis.Dial("tcp", addr) },
//    }
//  }
//
//  var (
//    pool *redis.Pool
//    redisServer = flag.String("redisServer", ":6379", "")
//  )
//
//  func main() {
//    flag.Parse()
//    pool = newPool(*redisServer)
//    ...
//  }
//
// A request handler gets a connection from the pool and closes the connection
// when the handler is done:
/*
请求处理程序从池中获取连接，并在处理程序完成后关闭该连接：
*/
//
//  func serveHome(w http.ResponseWriter, r *http.Request) {
//      conn := pool.Get()
//      defer conn.Close()
//      ...
//  }
//
// Use the Dial function to authenticate connections with the AUTH command or
// select a database with the SELECT command:
/*
使用拨号功能通过AUTH命令验证连接，或使用SELECT命令选择数据库：
*/
//
//  pool := &redis.Pool{
//    // Other pool configuration not shown in this example.
//    Dial: func () (redis.Conn, error) {
//      c, err := redis.Dial("tcp", server)
//      if err != nil {
//        return nil, err
//      }
//      if _, err := c.Do("AUTH", password); err != nil {
//        c.Close()
//        return nil, err
//      }
//      if _, err := c.Do("SELECT", db); err != nil {
//        c.Close()
//        return nil, err
//      }
//      return c, nil
//    },
//  }
//
// Use the TestOnBorrow function to check the health of an idle connection
// before the connection is returned to the application. This example PINGs
// connections that have been idle more than a minute:
/*
在连接返回到应用程序之前，使用TestOnBorrow函数检查空闲连接的运行状况。
此示例ping空闲时间超过一分钟的连接：
*/
//
//  pool := &redis.Pool{
//    // Other pool configuration not shown in this example.
//    TestOnBorrow: func(c redis.Conn, t time.Time) error {
//      if time.Since(t) < time.Minute {
//        return nil
//      }
//      _, err := c.Do("PING")
//      return err
//    },
//  }
//
type Pool struct {
	// Dial is an application supplied function for creating and configuring a
	// connection.
	//
	// The connection returned from Dial must not be in a special state
	// (subscribed to pubsub channel, transaction started, ...).
	/*
	拨号是应用程序提供的用于创建和配置连接的功能。
	从Dial返回的连接不能处于特殊状态(订阅了pubsub通道、事务已启动等)。
	*/
	Dial func() (Conn, error)

	// DialContext is an application supplied function for creating and configuring a
	// connection with the given context.
	//
	// The connection returned from Dial must not be in a special state
	// (subscribed to pubsub channel, transaction started, ...).
	/*
	DialContext是应用程序提供的函数，用于创建和配置与给定上下文的连接。
	从Dial返回的连接不能处于特殊状态(订阅了pubsub通道、事务已启动等)。
	*/
	DialContext func(ctx context.Context) (Conn, error)

	// TestOnBorrow is an optional application supplied function for checking
	// the health of an idle connection before the connection is used again by
	// the application. Argument t is the time that the connection was returned
	// to the pool. If the function returns an error, then the connection is
	// closed.
	/*
	TestOnBorrow是应用程序提供的可选函数，用于在应用程序再次使用空闲连接之前检查该连接的健康状况。
	参数t是连接返回池的时间。
	如果函数返回错误，则连接关闭。
	*/
	TestOnBorrow func(c Conn, t time.Time) error

	// Maximum number of idle connections in the pool.
	MaxIdle int

	// Maximum number of connections allocated by the pool at a given time.
	// When zero, there is no limit on the number of connections in the pool.
	/*
	池在给定时间分配的最大连接数。
	为零时，池中的连接数没有限制。
	*/
	MaxActive int

	// Close connections after remaining idle for this duration. If the value
	// is zero, then idle connections are not closed. Applications should set
	// the timeout to a value less than the server's timeout.
	/*
	在此持续时间内保持空闲状态后关闭连接。
	如果该值为零，则不会关闭空闲连接。
	应用程序应将超时值设置为小于服务器的超时值。
	*/
	IdleTimeout time.Duration

	// If Wait is true and the pool is at the MaxActive limit, then Get() waits
	// for a connection to be returned to the pool before returning.
	/*
	如果wait为true，并且池处于MaxActive限制，则get()将等待连接返回池，然后再返回。
	*/
	Wait bool

	// Close connections older than this duration. If the value is zero, then
	// the pool does not close connections based on age.
	/*
	关闭超过此持续时间的连接。
	如果该值为零，则池不会根据使用期限关闭连接。
	*/
	MaxConnLifetime time.Duration
	/*
	初始化场ch时设置为1
	*/
	chInitialized uint32 // set to 1 when field ch is initialized

	mu           sync.Mutex    // mu protects the following fields
	closed       bool          // set to true when the pool is closed.				    // 池关闭时设置为true。
	active       int           // the number of open connections in the pool			// 池中打开的连接数
	ch           chan struct{} // limits open connections when p.Wait is true			// 当p.Wait为true时限制打开的连接
	idle         idleList      // idle connections
	waitCount    int64         // total number of connections waited for.				// 等待的连接总数。
	waitDuration time.Duration // total time waited for new connections.				// 等待新连接的总时间。
}

// NewPool creates a new pool.
//
// Deprecated: Initialize the Pool directly as shown in the example.
func NewPool(newFn func() (Conn, error), maxIdle int) *Pool {
	return &Pool{Dial: newFn, MaxIdle: maxIdle}
}

// Get gets a connection. The application must close the returned connection.
// This method always returns a valid connection so that applications can defer
// error handling to the first use of the connection. If there is an error
// getting an underlying connection, then the connection Err, Do, Send, Flush
// and Receive methods return that error.
/*
GET获取连接。
应用程序必须关闭返回的连接。
此方法始终返回有效连接，以便应用程序可以将错误处理推迟到连接的第一次使用。
如果获取基础连接时出错，则连接Err、Do、Send、Flush和Receive方法将返回该错误。
*/
func (p *Pool) Get() Conn {
	// GetContext returns errorConn in the first argument when an error occurs.
	c, _ := p.GetContext(context.Background())
	return c
}

// GetContext gets a connection using the provided context.
//
// The provided Context must be non-nil. If the context expires before the
// connection is complete, an error is returned. Any expiration on the context
// will not affect the returned connection.
//
// If the function completes without error, then the application must close the
// returned connection.
/*
GetContext使用提供的上下文获取连接。
提供的上下文必须为非空。
如果上下文在连接完成之前过期，则会返回错误。
上下文的任何过期都不会影响返回的连接。
如果函数在没有错误的情况下完成，则应用程序必须关闭返回的连接。
*/
func (p *Pool) GetContext(ctx context.Context) (Conn, error) {
	// Wait until there is a vacant connection in the pool.
	waited, err := p.waitVacantConn(ctx)
	if err != nil {
		return errorConn{err}, err
	}

	p.mu.Lock()

	if waited > 0 {
		p.waitCount++
		p.waitDuration += waited
	}

	// Prune stale connections at the back of the idle list.
	if p.IdleTimeout > 0 {
		n := p.idle.count
		for i := 0; i < n && p.idle.back != nil && p.idle.back.t.Add(p.IdleTimeout).Before(nowFunc()); i++ {
			pc := p.idle.back
			p.idle.popBack()
			p.mu.Unlock()
			pc.c.Close()
			p.mu.Lock()
			p.active--
		}
	}

	// Get idle connection from the front of idle list.
	for p.idle.front != nil {
		pc := p.idle.front
		p.idle.popFront()
		p.mu.Unlock()
		if (p.TestOnBorrow == nil || p.TestOnBorrow(pc.c, pc.t) == nil) &&
			(p.MaxConnLifetime == 0 || nowFunc().Sub(pc.created) < p.MaxConnLifetime) {
			return &activeConn{p: p, pc: pc}, nil
		}
		pc.c.Close()
		p.mu.Lock()
		p.active--
	}

	// Check for pool closed before dialing a new connection.
	if p.closed {
		p.mu.Unlock()
		err := errors.New("redigo: get on closed pool")
		return errorConn{err}, err
	}

	// Handle limit for p.Wait == false.
	if !p.Wait && p.MaxActive > 0 && p.active >= p.MaxActive {
		p.mu.Unlock()
		return errorConn{ErrPoolExhausted}, ErrPoolExhausted
	}

	p.active++
	p.mu.Unlock()
	c, err := p.dial(ctx)
	if err != nil {
		c = nil
		p.mu.Lock()
		p.active--
		if p.ch != nil && !p.closed {
			p.ch <- struct{}{}
		}
		p.mu.Unlock()
		return errorConn{err}, err
	}
	return &activeConn{p: p, pc: &poolConn{c: c, created: nowFunc()}}, nil
}

// PoolStats contains pool statistics.
// PoolStats包含池统计信息。
type PoolStats struct {
	// ActiveCount is the number of connections in the pool. The count includes
	// idle connections and connections in use.
	ActiveCount int
	// IdleCount is the number of idle connections in the pool.
	IdleCount int

	// WaitCount is the total number of connections waited for.
	// This value is currently not guaranteed to be 100% accurate.
	WaitCount int64

	// WaitDuration is the total time blocked waiting for a new connection.
	// This value is currently not guaranteed to be 100% accurate.
	WaitDuration time.Duration
}

// Stats returns pool's statistics.
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	stats := PoolStats{
		ActiveCount:  p.active,
		IdleCount:    p.idle.count,
		WaitCount:    p.waitCount,
		WaitDuration: p.waitDuration,
	}
	p.mu.Unlock()

	return stats
}

// ActiveCount returns the number of connections in the pool. The count
// includes idle connections and connections in use.
/*
ActiveCount返回池中的连接数。
该计数包括空闲连接和正在使用的连接。
*/
func (p *Pool) ActiveCount() int {
	p.mu.Lock()
	active := p.active
	p.mu.Unlock()
	return active
}

// IdleCount returns the number of idle connections in the pool.
func (p *Pool) IdleCount() int {
	p.mu.Lock()
	idle := p.idle.count
	p.mu.Unlock()
	return idle
}

// Close releases the resources used by the pool.
func (p *Pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.active -= p.idle.count
	pc := p.idle.front
	p.idle.count = 0
	p.idle.front, p.idle.back = nil, nil
	if p.ch != nil {
		close(p.ch)
	}
	p.mu.Unlock()
	for ; pc != nil; pc = pc.next {
		pc.c.Close()
	}
	return nil
}

func (p *Pool) lazyInit() {
	// Fast path.
	if atomic.LoadUint32(&p.chInitialized) == 1 {
		return
	}
	// Slow path.
	p.mu.Lock()
	if p.chInitialized == 0 {
		p.ch = make(chan struct{}, p.MaxActive)
		if p.closed {
			close(p.ch)
		} else {
			for i := 0; i < p.MaxActive; i++ {
				p.ch <- struct{}{}
			}
		}
		atomic.StoreUint32(&p.chInitialized, 1)
	}
	p.mu.Unlock()
}

// waitVacantConn waits for a vacant connection in pool if waiting
// is enabled and pool size is limited, otherwise returns instantly.
// If ctx expires before that, an error is returned.
//
// If there were no vacant connection in the pool right away it returns the time spent waiting
// for that connection to appear in the pool.
/*
如果启用了等待并且池大小受限，waitVacantConn将等待池中的空闲连接，否则立即返回。
如果CTX在此之前过期，则返回错误。
如果池中立即没有空闲连接，则返回等待该连接出现在池中所花费的时间。
*/
func (p *Pool) waitVacantConn(ctx context.Context) (waited time.Duration, err error) {
	if !p.Wait || p.MaxActive <= 0 {
		// No wait or no connection limit.
		return 0, nil
	}

	p.lazyInit()

	// wait indicates if we believe it will block so its not 100% accurate
	// however for stats it should be good enough.
	/*
	等待指示我们是否相信它会阻塞，所以它不是100%！A(缺少)精确度，但是对于统计数据，它应该足够好了。
	*/
	wait := len(p.ch) == 0
	var start time.Time
	if wait {
		start = time.Now()
	}

	select {
	case <-p.ch:
		// Additionally check that context hasn't expired while we were waiting,
		// because `select` picks a random `case` if several of them are "ready".
		/*
		另外，在我们等待的时候检查上下文是否没有过期，因为`select`会随机选择一个`case`，如果其中有几个“就绪”的话。
		*/
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	if wait {
		return time.Since(start), nil
	}
	return 0, nil
}

func (p *Pool) dial(ctx context.Context) (Conn, error) {
	if p.DialContext != nil {
		return p.DialContext(ctx)
	}
	if p.Dial != nil {
		return p.Dial()
	}
	return nil, errors.New("redigo: must pass Dial or DialContext to pool")
}

func (p *Pool) put(pc *poolConn, forceClose bool) error {
	p.mu.Lock()
	if !p.closed && !forceClose {
		pc.t = nowFunc()
		p.idle.pushFront(pc)
		if p.idle.count > p.MaxIdle {
			pc = p.idle.back
			p.idle.popBack()
		} else {
			pc = nil
		}
	}

	if pc != nil {
		p.mu.Unlock()
		pc.c.Close()
		p.mu.Lock()
		p.active--
	}

	if p.ch != nil && !p.closed {
		p.ch <- struct{}{}
	}
	p.mu.Unlock()
	return nil
}

type activeConn struct {
	p     *Pool
	pc    *poolConn
	state int
}

var (
	sentinel     []byte
	sentinelOnce sync.Once
)

func initSentinel() {
	p := make([]byte, 64)
	if _, err := rand.Read(p); err == nil {
		sentinel = p
	} else {
		h := sha1.New()
		io.WriteString(h, "Oops, rand failed. Use time instead.")
		io.WriteString(h, strconv.FormatInt(time.Now().UnixNano(), 10))
		sentinel = h.Sum(nil)
	}
}

func (ac *activeConn) Close() error {
	pc := ac.pc
	if pc == nil {
		return nil
	}
	ac.pc = nil

	if ac.state&connectionMultiState != 0 {
		pc.c.Send("DISCARD")
		ac.state &^= (connectionMultiState | connectionWatchState)
	} else if ac.state&connectionWatchState != 0 {
		pc.c.Send("UNWATCH")
		ac.state &^= connectionWatchState
	}
	if ac.state&connectionSubscribeState != 0 {
		pc.c.Send("UNSUBSCRIBE")
		pc.c.Send("PUNSUBSCRIBE")
		// To detect the end of the message stream, ask the server to echo
		// a sentinel value and read until we see that value.
		/*
		要检测消息流的结尾，请要求服务器回显一个哨兵值并读取，直到我们看到该值。
		*/
		sentinelOnce.Do(initSentinel)
		pc.c.Send("ECHO", sentinel)
		pc.c.Flush()
		for {
			p, err := pc.c.Receive()
			if err != nil {
				break
			}
			if p, ok := p.([]byte); ok && bytes.Equal(p, sentinel) {
				ac.state &^= connectionSubscribeState
				break
			}
		}
	}
	pc.c.Do("")
	ac.p.put(pc, ac.state != 0 || pc.c.Err() != nil)
	return nil
}

func (ac *activeConn) Err() error {
	pc := ac.pc
	if pc == nil {
		return errConnClosed
	}
	return pc.c.Err()
}

func (ac *activeConn) Do(commandName string, args ...interface{}) (reply interface{}, err error) {
	pc := ac.pc
	if pc == nil {
		return nil, errConnClosed
	}
	ci := lookupCommandInfo(commandName)
	ac.state = (ac.state | ci.Set) &^ ci.Clear
	return pc.c.Do(commandName, args...)
}

func (ac *activeConn) DoWithTimeout(timeout time.Duration, commandName string, args ...interface{}) (reply interface{}, err error) {
	pc := ac.pc
	if pc == nil {
		return nil, errConnClosed
	}
	cwt, ok := pc.c.(ConnWithTimeout)
	if !ok {
		return nil, errTimeoutNotSupported
	}
	ci := lookupCommandInfo(commandName)
	ac.state = (ac.state | ci.Set) &^ ci.Clear
	return cwt.DoWithTimeout(timeout, commandName, args...)
}

func (ac *activeConn) Send(commandName string, args ...interface{}) error {
	pc := ac.pc
	if pc == nil {
		return errConnClosed
	}
	ci := lookupCommandInfo(commandName)
	ac.state = (ac.state | ci.Set) &^ ci.Clear
	return pc.c.Send(commandName, args...)
}

func (ac *activeConn) Flush() error {
	pc := ac.pc
	if pc == nil {
		return errConnClosed
	}
	return pc.c.Flush()
}

func (ac *activeConn) Receive() (reply interface{}, err error) {
	pc := ac.pc
	if pc == nil {
		return nil, errConnClosed
	}
	return pc.c.Receive()
}

func (ac *activeConn) ReceiveWithTimeout(timeout time.Duration) (reply interface{}, err error) {
	pc := ac.pc
	if pc == nil {
		return nil, errConnClosed
	}
	cwt, ok := pc.c.(ConnWithTimeout)
	if !ok {
		return nil, errTimeoutNotSupported
	}
	return cwt.ReceiveWithTimeout(timeout)
}

type errorConn struct{ err error }

func (ec errorConn) Do(string, ...interface{}) (interface{}, error) { return nil, ec.err }
func (ec errorConn) DoWithTimeout(time.Duration, string, ...interface{}) (interface{}, error) {
	return nil, ec.err
}
func (ec errorConn) Send(string, ...interface{}) error                     { return ec.err }
func (ec errorConn) Err() error                                            { return ec.err }
func (ec errorConn) Close() error                                          { return nil }
func (ec errorConn) Flush() error                                          { return ec.err }
func (ec errorConn) Receive() (interface{}, error)                         { return nil, ec.err }
func (ec errorConn) ReceiveWithTimeout(time.Duration) (interface{}, error) { return nil, ec.err }

type idleList struct {
	count       int
	front, back *poolConn
}

type poolConn struct {
	c          Conn
	t          time.Time
	created    time.Time
	next, prev *poolConn
}

func (l *idleList) pushFront(pc *poolConn) {
	pc.next = l.front
	pc.prev = nil
	if l.count == 0 {
		l.back = pc
	} else {
		l.front.prev = pc
	}
	l.front = pc
	l.count++
	return
}

func (l *idleList) popFront() {
	pc := l.front
	l.count--
	if l.count == 0 {
		l.front, l.back = nil, nil
	} else {
		pc.next.prev = nil
		l.front = pc.next
	}
	pc.next, pc.prev = nil, nil
}

func (l *idleList) popBack() {
	pc := l.back
	l.count--
	if l.count == 0 {
		l.front, l.back = nil, nil
	} else {
		pc.prev.next = nil
		l.back = pc.prev
	}
	pc.next, pc.prev = nil, nil
}
