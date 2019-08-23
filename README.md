# King Kai: HTTP benchmark reports using [Vegeta](https://github.com/tsenart/vegeta)

`kingkai` generates HTTP benchmark reports by comparing past and future attacks made by [`vegeta`](https://github.com/tsenart/vegeta).

<p align="center">
    <img alt="King Kai thinking" src="https://i.imgur.com/ncTEnuC.gif">
</p>

> He possesses great intelligence and knowledge about the universe, and specializes in universal telepathic links. Even though he was already weaker than Vegeta before he taught Goku, King Kai is still a veritable library of skills, techniques and literally centuries of wisdom. ([source](https://dragonball.fandom.com/wiki/King_Kai))

## Installation

Install [Go](https://golang.org), then run:

```sh
go get -u github.com/slimsag/kingkai
```

## Running

First use [`vegeta attack`](https://github.com/tsenart/vegeta) to produce some before and after gob files:

```sh
$ mkdir before
$ echo "GET https://google.com" | vegeta attack -duration=5s -name 'Google.com @ 50 QPS / 5s' > before/query1.bin

$ mkdir after
$ echo "GET https://google.com" | vegeta attack -duration=5s -name 'Google.com @ 50 QPS / 5s' > after/query1.bin

$ tree before/ after/
before/
└── query1.bin
after/
└── query1.bin

0 directories, 2 files
```

Then make use of `kingkai`:

```sh
$ kingkai before/ after/
```

Which outputs the following Markdown result:

> ### Google.com @ 2 QPS / 30s
>
> | Mean                    | P50                    | P95                     | P99                    | Max                    | Success Ratio |
> | ----------------------- | ---------------------- | ----------------------- | ---------------------- | ---------------------- | ------------- |
> | 178ms → 142ms (-20.22%) | 131ms → 130ms (-0.76%) | 436ms → 167ms (-61.70%) | 517ms → 613ms (18.57%) | 523ms → 655ms (25.24%) | 1% → 1%       |

## Thanks

@Andoryuuta for helping to write this all while 🛶 Canoeing across the atlantic.
