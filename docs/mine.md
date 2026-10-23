
# How I would structure things

Component {
  x,y
  x2,y2  // could be methods that return x + w and y + h
         // no harm storing the ints tho
  w,h
  focusable
  focusMode
  tabIndex
  parentComponent
}

Page {
  retainState // if switching to another page and back do we stay where we were?
}

