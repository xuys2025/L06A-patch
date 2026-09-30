// Measures the supported kernel interface; does not initialize audio/capture.
package main
import("fmt";"os";"time")
func main(){
 f,e:=os.OpenFile("/sys/devices/i2c-0/0-003a/led_rgb",os.O_WRONLY,0);if e!=nil{panic(e)};defer f.Close()
 defer func(){for p:=0;p<18;p++{fmt.Fprintf(f,"%d 0\n",p)}}()
 var total,max time.Duration; late:=0;start:=time.Now()
 for i:=0;i<200;i++{
  t:=time.Now()
  for p:=0;p<18;p++{v:=uint32(60+(i+p)%120);_,e=fmt.Fprintf(f,"%d %d\n",p,v|v<<8|v<<16);if e!=nil{panic(e)}}
  d:=time.Since(t);total+=d;if d>max{max=d};if d>25*time.Millisecond{late++}
  time.Sleep(time.Until(start.Add(time.Duration(i+1)*25*time.Millisecond)))
 }
 fmt.Printf("200 full frames via one sysfs fd: avg=%s max=%s late=%d elapsed=%s\n",total/200,max,late,time.Since(start))
}
