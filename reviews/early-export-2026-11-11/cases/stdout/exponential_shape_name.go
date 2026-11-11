package main
type (
a=struct{x,y byte}
b=struct{x,y a}
c=struct{x,y b}
d=struct{x,y c}
e=struct{x,y d}
f=struct{x,y e}
g=struct{x,y f}
h=struct{x,y g}
i=struct{x,y h}
j=struct{x,y i}
k=struct{x,y j}
l=struct{x,y k}
m=struct{x,y l}
n=struct{x,y m}
o=struct{x,y n}
p=struct{x,y o}
q=struct{x,y p}
r=struct{x,y q}
s=struct{x,y r}
)
//go:noinline
func z[T any](){}
func main(){z[s]()}
