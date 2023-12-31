package main

import (
	"crypto/md5"
	"fmt"
	"hash/crc32"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

/*
Тест проверяет, что в решении реализован правильный конвейер.

Неправильное поведение: накапливать результаты выполнения одной функции, а потом слать их в следующую.
Такой способ не позволяет запускать на конвейере бесконечные задачи

Правильное поведение: обеспечить беспрепятственный поток
*/
func TestPipeline(t *testing.T) {
	var ok = true
	var received uint32
	freeFlowJobs := []job{
		job(func(in, out chan interface{}) {
			out <- 1
			time.Sleep(10 * time.Millisecond)
			currRecieved := atomic.LoadUint32(&received)
			/*
			Если Вы накапливаете значения, то пока вся функция не отрабоатет - дальше они не пойдут.

			Тут я проверяю, что счетчик увеличился в следующей функции.
			Это значит что туда дошло значение прежде чем текущая функция отработала.
			*/
			if currRecieved == 0 {
				ok = false
			}
		}),
		job(func(in, out chan interface{}) {
			for range in {
				atomic.AddUint32(&received, 1)
			}
		}),
	}
	ExecutePipeline(freeFlowJobs...)
	if !ok || received == 0 {
		t.Errorf("no value free flow - dont collect them")
	}
}

func TestSigner(t *testing.T) {
	testExpected := "1173136728138862632818075107442090076184424490584241521304_1696913515191343735512658979631549563179965036907783101867_27225454331033649287118297354036464389062965355426795162684_29568666068035183841425683795340791879727309630931025356555_3994492081516972096677631278379039212655368881548151736_4958044192186797981418233587017209679042592862002427381542_4958044192186797981418233587017209679042592862002427381542"
	testResult := "NOT_SET"

	/*
	Это небольшая защита от попыток не вызывать мои функции расчета.
	Я преопределяю фукции на свои которые инкрементят локальный счетчик.
	Переопределение возможо, потому что я объявил функцию как переменную, в которой лежит функция.
	*/
	var (
		dataSignerSalt         string = "" // на сервере будет другое значение
		overheatLockCounter    uint32
		overheatUnlockCounter  uint32
		dataSignerMd5Counter   uint32
		dataSignerCrc32Counter uint32
	)
	OverheatLock = func() {
		atomic.AddUint32(&overheatLockCounter, 1)
		for {
			if swapped := atomic.CompareAndSwapUint32(&dataSignerOverheat, 0, 1); !swapped {
				fmt.Println("OverheatLock happened")
				time.Sleep(time.Second)
			} else {
				break
			}
		}
	}
	OverheatUnlock = func() {
		atomic.AddUint32(&overheatUnlockCounter, 1)
		for {
			if swapped := atomic.CompareAndSwapUint32(&dataSignerOverheat, 1, 0); !swapped {
				fmt.Println("OverheatUnlock happened")
				time.Sleep(time.Second)
			} else {
				break
			}
		}
	}
	DataSignerMd5 = func(data string) string {
		atomic.AddUint32(&dataSignerMd5Counter, 1)
		OverheatLock()
		defer OverheatUnlock()
		data += dataSignerSalt
		dataHash := fmt.Sprintf("%x", md5.Sum([]byte(data)))
		time.Sleep(10 * time.Millisecond)
		return dataHash
	}
	DataSignerCrc32 = func(data string) string {
		atomic.AddUint32(&dataSignerCrc32Counter, 1)
		data += dataSignerSalt
		crcH := crc32.ChecksumIEEE([]byte(data))
		dataHash := strconv.FormatUint(uint64(crcH), 10)
		time.Sleep(time.Second)
		return dataHash
	}

	inputData := []int{0, 1, 1, 2, 3, 5, 8}
	// inputData := []int{0,1}

	hashSignJobs := []job{
		job(func(in, out chan interface{}) {
			for _, fibNum := range inputData {
				out <- fibNum
			}
		}),
		job(SingleHash),
		job(MultiHash),
		job(CombineResults),
		job(func(in, out chan interface{}) {
			dataRaw := <-in
			data, ok := dataRaw.(string)
			if !ok {
				t.Error("cant convert result data to string")
			}
			testResult = data
		}),
	}

	start := time.Now()

	ExecutePipeline(hashSignJobs...)

	end := time.Since(start)

	expectedTime := 3 * time.Second

	if testExpected != testResult {
		t.Errorf("results not match\nGot: %v\nExpected: %v", testResult, testExpected)
	}

	if end > expectedTime {
		t.Errorf("execition too long\nGot: %s\nExpected: <%s", end, time.Second*3)
	}

	// 8, потому что 2 в SingleHash и 6 в MultiHash
	if int(overheatLockCounter) != len(inputData) ||
		int(overheatUnlockCounter) != len(inputData) ||
		int(dataSignerMd5Counter) != len(inputData) ||
		int(dataSignerCrc32Counter) != len(inputData)*8 {
		t.Errorf("not enough hash-func calls")
	}
}
