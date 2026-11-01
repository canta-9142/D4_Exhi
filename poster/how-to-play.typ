// nix shell --inputs-from . nixpkgs#typst -c typst compile poster/how-to-play.typ
#set document(title: "パケット配送ゲーム | プレイ方法")
#set page(
  "a1", margin: (x: 26mm, y: 24mm), fill: white,
  footer: align(right, text(size: 16pt)[01 / 02]),
)
#set text(font: "Noto Sans CJK JP", lang: "ja", size: 28pt, fill: black)
#set par(leading: 0.65em)

// 4枚共通の画像幅。高さ・ハイライト・文章の幅も追従する。
#let play-image-width = 216mm
#let play-image-radius = 6mm
#let play-caption-gap = 24mm

#align(center, block(width: 90%, height: 60mm, above: 0pt, below: 0pt)[
  #place(top + right, dy: -2mm, image("images/denkon-kun_color.png", width: 104mm))
  #place(top + left, dy: 20mm, block(width: 72%)[
    #align(left)[
      #text(size: 64pt, weight: "bold")[パケット配送ゲーム]#h(6mm)#text(size: 48pt, weight: "bold")[─ プレイ方法]
    ]
  ])
])
#line(length: 100%, stroke: 10pt + rgb("#a5a5a5"))
#v(24mm)

// Focus rectangles use the screenshots' common 480 x 320 coordinate system.
// The source images stay intact; all emphasis is drawn in Typst.
#let picture-frame(body) = block(
  width: play-image-width, height: play-image-width * 2 / 3,
  radius: play-image-radius, clip: true, above: 0pt, below: 0pt,
  body,
)

#let spotlight(path, focus) = {
  let width = play-image-width
  let unit = width / 480
  let (x, y, w, h) = focus
  picture-frame[
    #image(path, width: width)
    #for (mx, my, mw, mh) in (
      (0, 0, 480, y),
      (0, y + h, 480, 320 - y - h),
      (0, y, x, h),
      (x + w, y, 480 - x - w, h),
    ) {
      place(top + left, dx: mx * unit, dy: my * unit, rect(
        width: mw * unit, height: mh * unit, stroke: none,
        fill: black.transparentize(40%),
      ))
    }
    #for (spread, alpha, weight) in ((2mm, 88%, 8pt), (1mm, 72%, 5pt), (0mm, 0%, 4pt)) {
      place(top + left, dx: x * unit - spread, dy: y * unit - spread, rect(
        width: w * unit + 2 * spread, height: h * unit + 2 * spread,
        fill: none, stroke: weight + rgb("#ffe500").transparentize(alpha),
      ))
    }
  ]
}

#let caption(number, title, body) = grid(
  columns: (auto, auto), column-gutter: 8mm, align: left + horizon,
  text(size: 40pt, weight: "bold", number),
  grid.cell(
    stroke: (left: 1.5pt + rgb("#a5a5a5")), inset: (left: 8mm),
    stack(
      dir: ttb, spacing: 8mm,
      text(size: 40pt, weight: "bold", title),
      text(size: 30pt, body),
    ),
  ),
)

#let play-row(picture, description) = grid(
  columns: (play-image-width, 1fr), column-gutter: play-caption-gap, align: left + horizon,
  picture, description,
)

#let step-arrow() = play-row(
  block(width: play-image-width, height: 18mm, above: 0pt, below: 0pt,
    align(center + horizon, polygon(
      (0mm, 0mm), (14mm, 0mm), (7mm, 8mm),
      fill: rgb("#444444"), stroke: none,
    )),
  ),
  [],
)

#context {
  let captions = (
    caption([01], [パケットが生成される], [このパケットの目的地は「D」]),
    caption([02], [送るのは一番下から], [次に届いたパケットは上で待機]),
    caption([03], [送り先のボタンを押す], [例では「I」に対応するボタンを押す]),
    caption([04], [次のパケットも送ろう], [次の宛先を見て同じ操作を繰り返す]),
  )
  let caption-width = calc.max(..captions.map(it => measure(it).width))
  align(center, block(
    width: play-image-width + play-caption-gap + caption-width,
    height: 1fr, inset: (bottom: 30mm),
  )[
  // 画像サイズやcaptionの高さにかかわらず、4段を同じ高さに割り付ける。
  #grid(
    columns: (1fr,), rows: (1fr, auto, 1fr, auto, 1fr, auto, 1fr),
    align: left + horizon,
    play-row(
      spotlight("images/playing-1.png", (112, 164, 216, 72)),
      captions.at(0),
    ),
    step-arrow(),
    play-row(
      picture-frame(image("images/playing-2.png", width: play-image-width)),
      captions.at(1),
    ),
    step-arrow(),
    play-row(
      spotlight("images/playing-3.png", (118, 240, 124, 78)),
      captions.at(2),
    ),
    step-arrow(),
    play-row(
      picture-frame(image("images/playing-4.png", width: play-image-width)),
      captions.at(3),
    ),
  )
  ])
}
