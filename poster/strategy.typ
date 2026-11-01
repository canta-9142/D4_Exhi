// nix shell --inputs-from . nixpkgs#typst -c typst compile poster/strategy.typ
#set document(title: "パケット配送ゲーム | 戦略")
#set page(
  "a1", margin: (x: 26mm, y: 24mm), fill: white,
  footer: align(right, text(size: 16pt)[02 / 02]),
)
#set text(font: "Noto Sans CJK JP", lang: "ja", size: 28pt, fill: black)
#set par(leading: 0.65em)

#let strategy-image-width = 232mm
#let strategy-column-gap = 24mm

#align(center, block(width: 90%, height: 60mm, above: 0pt, below: 0pt)[
  #place(top + right, dy: -2mm, image("images/denkon-kun_color.png", width: 104mm))
  #place(top + left, dy: 20mm, block(width: 72%)[
    #align(left)[
      #text(size: 64pt, weight: "bold")[パケット配送ゲーム]#h(6mm)#text(size: 48pt, weight: "bold")[─ 戦略]
    ]
  ])
])
#line(length: 100%, stroke: 10pt + rgb("#a5a5a5"))

#let section-title(number, title) = grid(
  columns: (auto, auto), column-gutter: 8mm, align: left + horizon,
  text(size: 40pt, weight: "bold", number),
  grid.cell(
    stroke: (left: 1.5pt + rgb("#a5a5a5")), inset: (left: 8mm),
    text(size: 40pt, weight: "bold", title),
  ),
)

#let caption(title, body) = stack(
  dir: ttb, spacing: 8mm,
  text(size: 40pt, weight: "bold", title),
  text(size: 30pt, body),
)

#let strategy-figure(path, description) = stack(
  dir: ttb, spacing: 10mm,
  align(center, image(path, width: strategy-image-width)),
  align(center, description),
)

#context {
  let captions = (
    caption([C経由：転送3回], [A → B → C → D]),
    caption([I経由：転送2回], [A → I → D]),
    caption([4枠が埋まったら満杯], [空いている別の道を選ぼう]),
    caption([同じ場所に戻さない], [通ったノードに戻るとパケットは消える]),
  )
  let caption-width = calc.max(..captions.map(it => measure(it).width))
  let column-width = calc.max(strategy-image-width, caption-width)
  align(center, block(
    width: column-width * 2 + strategy-column-gap,
    height: 1fr, inset: (bottom: 6mm),
  )[
    // 見出し・図・説明を段ごとにまとめ、同じ高さの領域の中央に置く。
    #grid(
      columns: (1fr,), rows: (1fr, 1fr),
      align: center + horizon,
      grid(
        columns: (1fr, 1fr),
        column-gutter: strategy-column-gap, row-gutter: 12mm,
        align: center + top,
        grid.cell(colspan: 2, align: left, section-title([01], [近道を探そう])),
        strategy-figure("images/strategy-1.svg", captions.at(0)),
        strategy-figure("images/strategy-2.svg", captions.at(1)),
      ),
      grid(
        columns: (1fr, 1fr),
        column-gutter: strategy-column-gap, row-gutter: 12mm,
        align: center + top,
        grid.cell(align: left, section-title([02], [混雑を避けよう])),
        grid.cell(align: left, section-title([03], [逆戻りに注意])),
        strategy-figure("images/traffic-1.svg", captions.at(2)),
        strategy-figure("images/loop-1.svg", captions.at(3)),
      ),
    )
  ])
}
