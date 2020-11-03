# derive

[![Go Report Card](https://goreportcard.com/badge/github.com/256dpi/derive)](https://goreportcard.com/report/github.com/256dpi/derive)

**A small utility for building derivatives.** 

## Install

```
go get github.com/256dpi/derive
```

## Configuration

```yml
# run script when files change
- name: bar
  match:
    - "_foo"
  run:
    - cp -f _foo _bar

# run script or delegate to watch script
- name: baz
  run:
    - cp -f _foo _baz
  delegate:
    - watchman-wait . -p "_foo"; cp -f _foo _baz

# wildcard and exclusion rules
- name: exc
  match:
    - "_b*"
    - "!_bar"
  run:
    - echo '_baz updated'
```

## Usage

```
Usage of derive:
  -config string
        The configuration file. (default "./derive.yml")
  -watch
        Watch and derive incrementally.
```
