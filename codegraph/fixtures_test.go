// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

package codegraph

// langFixture is a small project in one language: a type with a method,
// a function and a file that calls both.
type langFixture struct {
	lang  string
	files map[string]string
	defs  []string // name:kind that must be found
	call  string   // a name that must have a strong use in the caller file
	in    string   // the caller file
}

var langFixtures = []langFixture{
	{lang: "go", in: "main.go", call: "Area", defs: []string{"Shape:struct", "Area:method", "NewShape:function", "Sizer:interface", "Limit:constant"},
		files: map[string]string{
			"shape.go": "package main\n\nconst Limit = 3\n\ntype Sizer interface {\n\tSize() int\n}\n\ntype Shape struct {\n\tW, H int\n}\n\nfunc NewShape(w, h int) *Shape {\n\treturn &Shape{W: w, H: h}\n}\n\nfunc (s *Shape) Area() int {\n\treturn s.W * s.H\n}\n",
			"main.go":  "package main\n\nfunc main() {\n\ts := NewShape(2, 3)\n\tprintln(s.Area())\n}\n",
		}},
	{lang: "typescript", in: "main.ts", call: "area", defs: []string{"Shape:class", "area:method", "newShape:function", "Sized:interface", "Unit:type", "double:function"},
		files: map[string]string{
			"shape.ts": "export interface Sized { size(): number }\nexport type Unit = 'px' | 'em'\nexport class Shape {\n  constructor(public w: number, public h: number) {}\n  area(): number {\n    return this.w * this.h\n  }\n}\nexport function newShape(w: number, h: number): Shape {\n  return new Shape(w, h)\n}\nexport const double = (n: number) => n * 2\n",
			"main.ts":  "import { newShape } from './shape'\nconst s = newShape(2, 3)\nconsole.log(s.area())\n",
		}},
	{lang: "tsx", in: "app.tsx", call: "Button", defs: []string{"Button:function", "App:function"},
		files: map[string]string{
			"button.tsx": "export function Button(props: { label: string }) {\n  return <button>{props.label}</button>\n}\n",
			"app.tsx":    "import { Button } from './button'\nexport function App() {\n  return <Button label=\"hi\" />\n}\n",
		}},
	{lang: "javascript", in: "main.js", call: "area", defs: []string{"Shape:class", "area:method", "newShape:function"},
		files: map[string]string{
			"shape.js": "class Shape {\n  constructor(w, h) { this.w = w; this.h = h }\n  area() {\n    return this.w * this.h\n  }\n}\nfunction newShape(w, h) {\n  return new Shape(w, h)\n}\nmodule.exports = { newShape }\n",
			"main.js":  "const { newShape } = require('./shape')\nconst s = newShape(2, 3)\nconsole.log(s.area())\n",
		}},
	{lang: "python", in: "main.py", call: "area", defs: []string{"Shape:class", "area:function", "new_shape:function"},
		files: map[string]string{
			"shape.py": "class Shape:\n    def __init__(self, w, h):\n        self.w = w\n        self.h = h\n\n    def area(self):\n        return self.w * self.h\n\n\ndef new_shape(w, h):\n    return Shape(w, h)\n",
			"main.py":  "from shape import new_shape\n\ns = new_shape(2, 3)\nprint(s.area())\n",
		}},
	{lang: "php", in: "main.php", call: "area", defs: []string{"Shape:class", "area:method", "new_shape:function", "Sized:interface"},
		files: map[string]string{
			"shape.php": "<?php\ninterface Sized { public function size(): int; }\nclass Shape {\n    public function __construct(private int $w, private int $h) {}\n    public function area(): int {\n        return $this->w * $this->h;\n    }\n}\nfunction new_shape(int $w, int $h): Shape {\n    return new Shape($w, $h);\n}\n",
			"main.php":  "<?php\nrequire 'shape.php';\n$s = new_shape(2, 3);\necho $s->area();\n",
		}},
	{lang: "rust", in: "main.rs", call: "area", defs: []string{"Shape:struct", "area:function", "new_shape:function", "Sized:trait"},
		files: map[string]string{
			"shape.rs": "pub trait Sized2 {}\npub trait Sized { fn size(&self) -> i32; }\npub struct Shape { w: i32, h: i32 }\nimpl Shape {\n    pub fn area(&self) -> i32 {\n        self.w * self.h\n    }\n}\npub fn new_shape(w: i32, h: i32) -> Shape {\n    Shape { w, h }\n}\n",
			"main.rs":  "mod shape;\nfn main() {\n    let s = shape::new_shape(2, 3);\n    let a = s.area();\n    println!(\"{}\", a);\n}\n",
		}},
	{lang: "java", in: "Main.java", call: "area", defs: []string{"Shape:class", "area:method", "Sized:interface"},
		files: map[string]string{
			"Shape.java": "interface Sized { int size(); }\npublic class Shape {\n    private int w, h;\n    public Shape(int w, int h) { this.w = w; this.h = h; }\n    public int area() {\n        return w * h;\n    }\n}\n",
			"Main.java":  "public class Main {\n    public static void main(String[] args) {\n        Shape s = new Shape(2, 3);\n        System.out.println(s.area());\n    }\n}\n",
		}},
	{lang: "ruby", in: "main.rb", call: "area", defs: []string{"Shape:class", "area:method", "Geometry:module"},
		files: map[string]string{
			"shape.rb": "module Geometry\n  class Shape\n    def initialize(w, h)\n      @w = w\n      @h = h\n    end\n\n    def area\n      @w * @h\n    end\n  end\nend\n",
			"main.rb":  "require_relative 'shape'\ns = Geometry::Shape.new(2, 3)\nputs s.area()\n",
		}},
	{lang: "c", in: "main.c", call: "shape_area", defs: []string{"shape:struct", "shape_area:function"},
		files: map[string]string{
			"shape.c": "struct shape {\n  int w, h;\n};\n\nint shape_area(struct shape *s) {\n  return s->w * s->h;\n}\n",
			"main.c":  "#include <stdio.h>\nint main(void) {\n  struct shape s = {2, 3};\n  printf(\"%d\", shape_area(&s));\n  return 0;\n}\n",
		}},
	{lang: "cpp", in: "main.cpp", call: "area", defs: []string{"Shape:class", "area:function", "geo:module"},
		files: map[string]string{
			"shape.cpp": "namespace geo {\nclass Shape {\npublic:\n  int w, h;\n  int area() {\n    return w * h;\n  }\n};\n}\n",
			"main.cpp":  "#include <iostream>\nint main() {\n  geo::Shape s{2, 3};\n  std::cout << s.area();\n}\n",
		}},
	{lang: "c_sharp", in: "Main.cs", call: "Area", defs: []string{"Shape:class", "Area:method", "ISized:interface"},
		files: map[string]string{
			"Shape.cs": "interface ISized { int Size(); }\npublic class Shape {\n    int w, h;\n    public Shape(int w, int h) { this.w = w; this.h = h; }\n    public int Area() {\n        return w * h;\n    }\n}\n",
			"Main.cs":  "class Program {\n    static void Main() {\n        var s = new Shape(2, 3);\n        System.Console.WriteLine(s.Area());\n    }\n}\n",
		}},
	{lang: "kotlin", in: "Main.kt", call: "area", defs: []string{"Shape:class", "area:function", "newShape:function"},
		files: map[string]string{
			"Shape.kt": "class Shape(val w: Int, val h: Int) {\n    fun area(): Int {\n        return w * h\n    }\n}\n\nfun newShape(w: Int, h: Int): Shape {\n    return Shape(w, h)\n}\n",
			"Main.kt":  "fun main() {\n    val s = newShape(2, 3)\n    println(s.area())\n}\n",
		}},
}
