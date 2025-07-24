module github.com/jbrzusto/shiftwrap/v2

go 1.24.2

replace radarcam.ca/shiftwrap => ./

#replace github.com/jbrzusto/timewarper => ../timewarper

require github.com/sixdouglas/suncalc v0.0.0-20250114185126-291b1938b70c

require github.com/jbrzusto/timewarper v0.0.0-20250508113930-f0e702816d3d

require gopkg.in/yaml.v3 v3.0.1 // indirect
