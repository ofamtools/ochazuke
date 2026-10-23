package ochazuke

import (
 "errors"
 "strings"
 "unicode"
)

func (in *Input) lineBounds(cursor int) (int,int) {
 cursor=max(0,min(cursor,len(in.Text)))
 start,end:=cursor,cursor
 for start>0 && in.Text[start-1]!='\n' { start-- }
 for end<len(in.Text) && in.Text[end]!='\n' { end++ }
 return start,end
}

func (in *Input) clampCursor() {
 in.Cursor=max(0,min(in.Cursor,len(in.Text)))
 if in.Mode=="normal" {
  start,end:=in.lineBounds(in.Cursor)
  if end>start && in.Cursor==end { in.Cursor-- }
 }
}

func wordClass(ch rune) int {
 if unicode.IsSpace(ch) { return 0 }
 if unicode.IsLetter(ch) || unicode.IsDigit(ch) || unicode.IsMark(ch) || ch=='_' { return 1 }
 return 2
}

func (in *Input) wordForward(cursor int) int {
 if cursor>=len(in.Text) { return len(in.Text) }
 class:=wordClass(in.Text[cursor])
 for cursor<len(in.Text) && wordClass(in.Text[cursor])==class { cursor++ }
 for cursor<len(in.Text) && wordClass(in.Text[cursor])==0 { cursor++ }
 return cursor
}

func (in *Input) wordBackward(cursor int) int {
 if cursor==0 { return 0 }
 cursor--
 for cursor>0 && wordClass(in.Text[cursor])==0 { cursor-- }
 class:=wordClass(in.Text[cursor])
 for cursor>0 && wordClass(in.Text[cursor-1])==class { cursor-- }
 return cursor
}

func (in *Input) wordEnd(cursor int) int {
 if cursor>=len(in.Text)-1 { return max(0,len(in.Text)-1) }
 cursor++
 for cursor<len(in.Text)-1 && wordClass(in.Text[cursor])==0 { cursor++ }
 class:=wordClass(in.Text[cursor])
 for cursor<len(in.Text)-1 && wordClass(in.Text[cursor+1])==class { cursor++ }
 return cursor
}

func (in *Input) readlineWordForward(cursor int) int {
 for cursor<len(in.Text) && wordClass(in.Text[cursor])!=1 { cursor++ }
 for cursor<len(in.Text) && wordClass(in.Text[cursor])==1 { cursor++ }
 return cursor
}

func (in *Input) readlineWordBackward(cursor int) int {
 for cursor>0 && wordClass(in.Text[cursor-1])!=1 { cursor-- }
 for cursor>0 && wordClass(in.Text[cursor-1])==1 { cursor-- }
 return cursor
}

func (in *Input) move(motion string,steps,line int) error {
 if steps<1 { return errors.New("steps must be positive") }
 if line<0 { return errors.New("line must be positive or omitted") }
 switch motion {
 case "left","right","up","down","word-forward","word-backward","word-end","word-next","word-previous","line-start","line-end","first-line","last-line":
 default: return errors.New("unknown input motion")
 }
 in.resetKeys()
 in.clampCursor()
 start,end:=in.lineBounds(in.Cursor)
 steps=min(steps,len(in.Text)+1)
 if motion!="up" && motion!="down" { in.goalColumn=-1 }
 switch motion {
 case "left":
  lower:=0
  if in.Mode=="normal" { lower=start }
  in.Cursor=max(lower,in.Cursor-steps)
 case "right":
  upper:=len(in.Text)
  if in.Mode=="normal" { upper=max(start,end-1) }
  in.Cursor=min(upper,in.Cursor+steps)
 case "up","down":
  if in.goalColumn<0 { in.goalColumn=in.Cursor-start }
  for ;steps>0;steps-- {
   if motion=="up" {
    if start==0 { break }
    start,end=in.lineBounds(start-1)
   } else {
    if end==len(in.Text) { break }
    start,end=in.lineBounds(end+1)
   }
  }
  limit:=end
  if in.Mode=="normal" { limit=max(start,end-1) }
  in.Cursor=min(start+in.goalColumn,limit)
 case "word-forward","word-backward","word-end","word-next","word-previous":
  for ;steps>0;steps-- {
   old:=in.Cursor
   switch motion {
   case "word-forward": in.Cursor=in.wordForward(in.Cursor)
   case "word-backward": in.Cursor=in.wordBackward(in.Cursor)
   case "word-end": in.Cursor=in.wordEnd(in.Cursor)
   case "word-next": in.Cursor=in.readlineWordForward(in.Cursor)
   case "word-previous": in.Cursor=in.readlineWordBackward(in.Cursor)
   }
   if old==in.Cursor { break }
  }
 case "line-start": in.Cursor=start
 case "line-end":
  for ;steps>1 && end<len(in.Text);steps-- { start,end=in.lineBounds(end+1) }
  in.Cursor=end
 case "first-line","last-line":
  if line==0 && motion=="last-line" {
   in.Cursor=len(in.Text)
   in.Cursor,_=in.lineBounds(in.Cursor)
  } else {
   line=max(1,line)
   in.Cursor=0
   for ;line>1;line-- {
    _,end=in.lineBounds(in.Cursor)
    if end==len(in.Text) { break }
    in.Cursor=end+1
   }
  }
 }
 in.clampCursor()
 return nil
}

func (in *Input) begin(position string) error {
 switch position {
 case "insert","append","first","end","below","above":
 default: return errors.New("unknown insertion position")
 }
 if (position=="below" || position=="above") && !in.Multiline { return errors.New("single-line input cannot open a new line") }
 in.resetKeys()
 in.SetMode("insert")
 // An explicit begin after undo starts a new insert group even if the mode stayed insert.
 if !in.groupOpen { in.groupOpen=true; in.groupNode=-1 }
 start,end:=in.lineBounds(in.Cursor)
 switch position {
 case "append": if in.Cursor<end { in.Cursor++ }
 case "first":
  in.Cursor=start
  for in.Cursor<end && unicode.IsSpace(in.Text[in.Cursor]) { in.Cursor++ }
 case "end": in.Cursor=end
 case "below":
  if err:=in.Replace(end,end,"\n");err!=nil { return err }
 case "above":
  if err:=in.Replace(start,start,"\n");err!=nil { return err }
  in.Cursor=start
 }
 in.goalColumn=-1
 in.clampCursor()
 return nil
}

func (in *Input) deleteRunes(direction string,steps int) error {
 if steps<1 { return errors.New("steps must be positive") }
 if direction!="forward" && direction!="backward" { return errors.New("unknown delete direction") }
 in.resetKeys()
 start,end:=in.Cursor,in.Cursor
 lower,upper:=0,len(in.Text)
 if in.Mode=="normal" { lower,upper=in.lineBounds(in.Cursor) }
 steps=min(steps,len(in.Text))
 if direction=="backward" { start=max(lower,start-steps) } else { end=min(upper,end+steps) }
 return in.Replace(start,end,"")
}

func (in *Input) kill(motion string) error {
 start,end:=in.Cursor,in.Cursor
 lineStart,lineEnd:=in.lineBounds(in.Cursor)
 switch motion {
 case "word-backward":
  for start>0 && unicode.IsSpace(in.Text[start-1]) { start-- }
  for start>0 && !unicode.IsSpace(in.Text[start-1]) { start-- }
 case "line-start": start=lineStart
 case "line-end":
  end=lineEnd
  if start==end && end<len(in.Text) && in.Multiline { end++ }
 default: return errors.New("unknown kill motion")
 }
 in.resetKeys()
 if start==end { return nil }
 killed:=string(in.Text[start:end])
 if err:=in.Replace(start,end,"");err!=nil { return err }
 in.Kill=killed
 return nil
}

func (in *Input) prefix(key string) error {
 if in.Mode!="normal" { return errors.New("input prefixes require normal mode") }
 if key=="g" { in.Pending="g"; return nil }
 if len(key)!=1 || key[0]<'0' || key[0]>'9' { return errors.New("prefix must be g or a digit") }
 digit:=int(key[0]-'0')
 if in.Count>100000 || in.Count*10+digit>1000000 { return errors.New("count exceeds 1000000") }
 in.Count=in.Count*10+digit
 return nil
}

// HandleInputKey is called only after Runtime.HandleBinding declines a key.
// It interprets keys, but every state mutation is a registered operation.
func HandleInputKey(r *Runtime,key string) (bool,error) {
 in:=r.Inputs[r.Context().InputID]
 if in==nil { return false,nil }
 execute:=func(verb,noun string,args Arguments)(bool,error) {
  _,err:=r.Execute(Request{Verb:verb,Noun:noun,Target:"focused-input",Args:args})
  return true,err
 }
 command:=func(verb string,args Arguments)(bool,error) { return execute(verb,"input",args) }
 move:=func(motion string)(bool,error) { return command("move",Arguments{"motion":motion}) }
 if in.Mode=="insert" {
  switch key {
  case "esc": return execute("set","input-mode",Arguments{"mode":"normal"})
  case "ctrl+a","home": return move("line-start")
  case "ctrl+e","end": return move("line-end")
  case "ctrl+b","left": return move("left")
  case "ctrl+f","right": return move("right")
  case "ctrl+p","up": return move("up")
  case "ctrl+n","down": return move("down")
  case "alt+b": return move("word-previous")
  case "alt+f": return move("word-next")
  case "ctrl+w": return command("kill",Arguments{"motion":"word-backward"})
  case "ctrl+u": return command("kill",Arguments{"motion":"line-start"})
  case "ctrl+k": return command("kill",Arguments{"motion":"line-end"})
  case "ctrl+y": return command("yank",nil)
  case "backspace","ctrl+h": return command("delete",Arguments{"direction":"backward"})
  case "delete","ctrl+d": return command("delete",nil)
  case "enter":
   if !in.Multiline { return false,nil }
   return command("insert",Arguments{"text":"\n"})
  case "space": return command("insert",Arguments{"text":" "})
  }
  // Bubble Tea v1 brackets KeyRunes paste text in KeyMsg.String().
 if strings.HasPrefix(key,"[") && strings.HasSuffix(key,"]") && len(key)>=2 {
  text:=strings.ReplaceAll(key[1:len(key)-1],"\r\n","\n")
  return command("insert",Arguments{"text":strings.ReplaceAll(text,"\r","\n")})
 }
 if literalInput(key) { return command("insert",Arguments{"text":key}) }
  return false,nil
 }
 if in.Mode!="normal" { return false,nil }
 if key=="esc" { return command("cancel",nil) }
 if in.Pending=="g" && key=="g" {
  return command("move",Arguments{"motion":"first-line","line":max(1,in.Count)})
 }
 if in.Pending!="" {
  if _,err:=command("cancel",nil);err!=nil { return true,err }
 }
 if len(key)==1 && key[0]>='0' && key[0]<='9' && (key!="0" || in.Count>0) {
  return command("prefix",Arguments{"key":key})
 }
 steps:=max(1,in.Count)
 motion:=""
 switch key {
 case "g": return command("prefix",Arguments{"key":"g"})
 case "h","left": motion="left"
 case "j","down": motion="down"
 case "k","up": motion="up"
 case "l","right": motion="right"
 case "w": motion="word-forward"
 case "b": motion="word-backward"
 case "e": motion="word-end"
 case "0","home": motion="line-start"
 case "$","end": motion="line-end"
 case "G": return command("move",Arguments{"motion":"last-line","line":in.Count})
 case "i": return command("begin",Arguments{"position":"insert"})
 case "a": return command("begin",Arguments{"position":"append"})
 case "I": return command("begin",Arguments{"position":"first"})
 case "A": return command("begin",Arguments{"position":"end"})
 case "o": return command("begin",Arguments{"position":"below"})
 case "O": return command("begin",Arguments{"position":"above"})
 case "x","delete": return command("delete",Arguments{"steps":steps})
 case "u": return command("undo",Arguments{"steps":steps})
 case "ctrl+r": return command("redo",Arguments{"steps":steps})
 default:
  if in.Count!=0 || in.Pending!="" { return command("cancel",nil) }
  return false,nil
 }
 return command("move",Arguments{"motion":motion,"steps":steps})
}

func literalInput(key string) bool {
 if key=="" { return false }
 if strings.HasPrefix(key,"ctrl+") || strings.HasPrefix(key,"alt+") || strings.HasPrefix(key,"shift+") { return false }
 switch key {
 case "esc","enter","tab","shift+tab","backspace","delete","insert","home","end","pgup","pgdown","up","down","left","right","f1","f2","f3","f4","f5","f6","f7","f8","f9","f10","f11","f12": return false
 }
 for _,ch:=range key { if unicode.IsControl(ch) && ch!='\n' && ch!='\r' && ch!='\t' { return false } }
 return true
}
