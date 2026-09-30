// Local-only silent WAV Range probe. It never opens a microphone.
package main
import("log";"net/http")
func main(){
 log.Fatal(http.ListenAndServe("127.0.0.1:18090", http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  log.Printf("method=%s range=%q",r.Method,r.Header.Get("Range"))
  http.ServeFile(w,r,"/tmp/seek-probe.wav")
 })))
}
