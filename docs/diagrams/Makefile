OBJ=$(patsubst %.tex, %.png, $(wildcard *.tex))

.PHONY: all

all: $(OBJ)

%.pdf: %.tex
	lualatex $< -shell-escape $@

%.png: %.pdf
	convert -flatten -density 300 $< -quality 90 $@
