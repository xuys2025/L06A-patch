// Bounded AW20054 register readback/timing probe. No microphone or audio access.
package main

import (
 "fmt"
 "os"
 "runtime"
 "syscall"
 "time"
 "unsafe"
)

type msg struct { Addr, Flags, Len uint16; Buf *byte }
type rdwr struct { Msgs *msg; Count uint32 }

func transfer(f *os.File, data [][]byte, reads map[int]bool) error {
 m := make([]msg,len(data))
 for i,b := range data { m[i] = msg{Addr:0x3a, Len:uint16(len(b)), Buf:&b[0]}; if reads[i] { m[i].Flags=1 } }
 req:=rdwr{&m[0],uint32(len(m))}
 n,_,e:=syscall.Syscall(syscall.SYS_IOCTL,f.Fd(),0x0707,uintptr(unsafe.Pointer(&req)))
 runtime.KeepAlive(data); runtime.KeepAlive(m)
 if e!=0 { return e }; if int(n)!=len(m) { return fmt.Errorf("partial transfer %d/%d",n,len(m)) }; return nil
}

func readRows(f *os.File) ([][]byte,error) {
 result:=[][]byte{{0xf0,0xc1}}
 for row:=0;row<6;row++ {
  data:=[][]byte{{0xf0,0xc1}};reads:=map[int]bool{};b:=[]byte{byte(row*12)}
  for col:=0;col<9;col++ {data=append(data,[]byte{byte(row*12+col)},[]byte{0});reads[len(data)-1]=true}
  if err:=transfer(f,data,reads);err!=nil{return nil,err}
  for col:=0;col<9;col++ {b=append(b,data[col*2+2][0])};result=append(result,b)
 }
 return result,nil
}

func run() error {
 f,e:=os.OpenFile("/dev/i2c-0",os.O_RDWR,0); if e!=nil{return e}; defer f.Close()
 original,e:=readRows(f); if e!=nil{return e}
 defer func(){if e:=transfer(f,original,nil);e!=nil{fmt.Println("RESTORE ERROR",e)}}()
 pattern:=[][]byte{{0xf0,0xc1}}
 for row:=0;row<6;row++ { b:=[]byte{byte(row*12)};for col:=0;col<9;col++{b=append(b,byte((row*9+col)%64))};pattern=append(pattern,b) }
 if e=transfer(f,pattern,nil);e!=nil{return e}
 actual,e:=readRows(f);if e!=nil{return e}
 for row:=1;row<=6;row++ { for col:=1;col<=9;col++ {if actual[row][col]!=pattern[row][col] {return fmt.Errorf("mismatch row=%d col=%d want=%d got=%d",row,col,pattern[row][col],actual[row][col])}} }
 fmt.Println("54 channel burst write/readback: PASS")
 var total,max time.Duration; start:=time.Now()
 for i:=0;i<200;i++ { t:=time.Now(); if e=transfer(f,pattern,nil);e!=nil{return e};d:=time.Since(t);total+=d;if d>max{max=d};time.Sleep(time.Until(start.Add(time.Duration(i+1)*25*time.Millisecond))) }
 fmt.Printf("200 frames: io average=%s max=%s elapsed=%s\n",total/200,max,time.Since(start))
 return nil
}
func main(){if e:=run();e!=nil{fmt.Fprintln(os.Stderr,e);os.Exit(1)}}
