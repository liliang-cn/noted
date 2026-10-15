import SwiftUI

// Small reusable views from the prototype. All take plain values; none reads AppData.

// MARK: - space

/// The 8 pt space dot (个人 rust, 工作 blue).
struct SpaceDot: View {
    let space: Noted_V1_Space
    var size: CGFloat = 8
    var body: some View {
        Circle().fill(space.tint).frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}

/// "● 个人" capsule on a tinted background (detail headers).
struct SpaceTag: View {
    let space: Noted_V1_Space
    var body: some View {
        let s: Noted_V1_Space = space == .work ? .work : .life
        HStack(spacing: 5) {
            SpaceDot(space: s, size: 7)
            Text(s.label).font(.footnote.weight(.semibold))
        }
        .padding(.horizontal, 9).frame(height: 24)
        .foregroundStyle(s.tint)
        .background(s.tint.wash(s == .work ? 0.11 : 0.12), in: Capsule())
    }
}

/// A neutral capsule tag in the same shape as SpaceTag ("置顶", "已归档", "当前").
struct InfoTag: View {
    let text: String
    var systemImage: String?
    var tint: Color = .secondaryText
    var background: Color = .fillTrack
    var body: some View {
        HStack(spacing: 4) {
            if let systemImage { Image(systemName: systemImage).font(.system(size: 11, weight: .semibold)) }
            Text(text).font(.footnote.weight(.semibold))
        }
        .padding(.horizontal, 9).frame(height: 24)
        .foregroundStyle(tint)
        .background(background, in: Capsule())
    }
}

// MARK: - progress

/// A thin rounded bar. `height` 4 in lists, 8 (`.large`) in detail cards. Animates changes.
struct ProgressBar: View {
    var value: Double
    var tint: Color? = nil
    var height: CGFloat = 4

    static let large: CGFloat = 8

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(.fillTrack)
                Capsule()
                    .fill(tint.map { AnyShapeStyle($0) } ?? AnyShapeStyle(.tint))
                    .frame(width: max(0, g.size.width * min(max(value, 0), 1)))
            }
        }
        .frame(height: height)
        .animation(.smooth(duration: 0.5), value: value)
        .accessibilityElement()
        .accessibilityValue("\(Int((min(max(value, 0), 1) * 100).rounded()))%")
    }
}

/// A ring that fills clockwise from the top. Animates when `value` changes; with `animateOnAppear`
/// it also grows from 0 the first time it shows.
struct ProgressRing<Center: View>: View {
    var value: Double
    var lineWidth: CGFloat = 4.5
    var tint: Color? = nil
    var animateOnAppear = false
    @ViewBuilder var center: Center
    @State private var shown: Double?

    var body: some View {
        let v = min(max(shown ?? value, 0), 1)
        ZStack {
            Circle().stroke(.fillTrack, lineWidth: lineWidth)
            Circle()
                .trim(from: 0, to: v)
                .stroke(tint.map { AnyShapeStyle($0) } ?? AnyShapeStyle(.tint),
                        style: StrokeStyle(lineWidth: lineWidth, lineCap: .round))
                .rotationEffect(.degrees(-90))
            center
        }
        .padding(lineWidth / 2)
        .onAppear {
            guard animateOnAppear, shown == nil else { return }
            shown = 0
            withAnimation(.smooth(duration: 0.7).delay(0.05)) { shown = value }
        }
        .onChange(of: value) { _, new in
            withAnimation(.smooth(duration: 0.7)) { shown = new }
        }
        .accessibilityElement(children: .combine)
        .accessibilityValue("\(Int((min(max(value, 0), 1) * 100).rounded()))%")
    }
}

extension ProgressRing where Center == EmptyView {
    init(value: Double, lineWidth: CGFloat = 4.5, tint: Color? = nil, animateOnAppear: Bool = false) {
        self.init(value: value, lineWidth: lineWidth, tint: tint, animateOnAppear: animateOnAppear) { EmptyView() }
    }
}

/// A small line chart of recent values with a dot on the last one (goal rows).
struct Sparkline: View {
    let values: [Double]
    var tint: Color = .success
    var body: some View {
        GeometryReader { g in
            let pts = points(in: g.size)
            ZStack {
                Path { p in
                    guard let first = pts.first else { return }
                    p.move(to: first)
                    for pt in pts.dropFirst() { p.addLine(to: pt) }
                }
                .stroke(tint, style: StrokeStyle(lineWidth: 1.8, lineCap: .round, lineJoin: .round))
                if let last = pts.last {
                    Circle().fill(tint).frame(width: 5.2, height: 5.2).position(last)
                }
            }
        }
        .accessibilityHidden(true)
    }

    private func points(in size: CGSize) -> [CGPoint] {
        guard values.count > 1, let lo = values.min(), let hi = values.max() else {
            return values.isEmpty ? [] : [CGPoint(x: size.width - 2, y: size.height / 2)]
        }
        let range = hi - lo == 0 ? 1 : hi - lo
        return values.enumerated().map { i, v in
            CGPoint(x: 2 + CGFloat(i) / CGFloat(values.count - 1) * (size.width - 4),
                    y: size.height - 2 - CGFloat((v - lo) / range) * (size.height - 4))
        }
    }
}

// MARK: - headers, empty

/// The 20 pt bold section title with an optional "全部 ›" on the right. Use as a List section
/// header: `Section { … } header: { SectionHeader("目标", action: "全部") { router.push(.goals) } }`.
struct SectionHeader: View {
    let title: String
    var actionTitle: String?
    var action: (() -> Void)?

    init(_ title: String, action actionTitle: String? = nil, perform action: (() -> Void)? = nil) {
        self.title = title
        self.actionTitle = actionTitle
        self.action = action
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title).font(.title3.weight(.bold)).foregroundStyle(Color(uiColor: .label))
                .accessibilityAddTraits(.isHeader)
            Spacer()
            if let actionTitle, let action {
                Button(action: action) {
                    HStack(spacing: 2) {
                        Text(actionTitle)
                        Image(systemName: "chevron.right").font(.system(size: 13, weight: .semibold))
                    }
                    .font(.subheadline)
                    .foregroundStyle(.tint)
                    .frame(minHeight: 30)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        .textCase(nil)
        .listRowInsets(EdgeInsets(top: 8, leading: 4, bottom: 8, trailing: 0))
    }
}

/// Centered gray text for an empty list or section ("今天没有安排").
struct EmptyState: View {
    let text: String
    var systemImage: String?
    var body: some View {
        VStack(spacing: 8) {
            if let systemImage { Image(systemName: systemImage).font(.title2).foregroundStyle(.tertiaryText) }
            Text(text).font(.subheadline).foregroundStyle(.secondaryText).multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 22)
    }
}

// MARK: - chips

/// A 34 pt capsule: white with a hairline, or filled with the accent when on.
struct Chip: View {
    let title: String
    var isOn = false
    var filled = false   // gray fill instead of white when off (`.chip.f`)
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(.subheadline)
                .lineLimit(1)
                .padding(.horizontal, 14)
                .frame(height: 34)
                .foregroundStyle(isOn ? AnyShapeStyle(.white) : AnyShapeStyle(.primary))
                .background {
                    if isOn { Capsule().fill(.tint) }
                    else if filled { Capsule().fill(.fillTrack) }
                    else { Capsule().fill(.white).overlay(Capsule().strokeBorder(.hairline, lineWidth: 0.5)) }
                }
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isOn ? .isSelected : [])
    }
}

/// A horizontally scrolling row of chips, 16 pt from the edges. Put it in a List row with
/// `.listRowInsets(EdgeInsets())` and `.listRowBackground(Color.clear)`, or anywhere else.
struct ChipRow<Content: View>: View {
    @ViewBuilder var content: Content
    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 8) { content }.padding(.horizontal, 16)
        }
        .scrollIndicators(.hidden)
        .scrollBounceBehavior(.basedOnSize)
    }
}

/// 今天 · 明天 · 下周一 · 选择… · 无日期, as on 新建. `onPick` runs for 选择… (open a date picker);
/// the chip then shows the chosen day ("10月18日").
struct QuickDateChips: View {
    @Binding var date: Date?
    var allowsNone = true
    var onPick: () -> Void
    var now: Date = .now

    var body: some View {
        let today = now.startOfDay
        let quick: [(String, Date)] = [("今天", today), ("明天", today.adding(days: 1)), ("下周一", today.nextMonday)]
        let other = date.map { d in !quick.contains { $0.1.isSameDay(as: d) } } ?? false
        ChipRow {
            ForEach(quick, id: \.0) { label, d in
                Chip(title: label, isOn: date.map { $0.isSameDay(as: d) } ?? false) { date = keepTime(d) }
            }
            Chip(title: other ? Fmt.md(date!) : "选择…", isOn: other, action: onPick)
            if allowsNone { Chip(title: "无日期", isOn: date == nil) { date = nil } }
        }
    }

    /// A new day keeps the time of day already chosen.
    private func keepTime(_ day: Date) -> Date {
        guard let d = date, d.minuteOfDay != 0 else { return day }
        return day.at(hour: d.minuteOfDay / 60, minute: d.minuteOfDay % 60)
    }
}

// MARK: - pills and buttons

/// The 30 pt rounded "pill" button (`.pill`): gray, or accent-tinted with `accent`.
struct PillButtonStyle: ButtonStyle {
    var accent = false
    var height: CGFloat = 30
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.subheadline)
            .lineLimit(1)
            .padding(.horizontal, 12)
            .frame(height: height)
            .foregroundStyle(accent ? AnyShapeStyle(.tint) : AnyShapeStyle(.primary))
            .background(RoundedRectangle(cornerRadius: 8).fill(accent ? AnyShapeStyle(.tint.opacity(0.12)) : AnyShapeStyle(.fillTrack)))
            .opacity(configuration.isPressed ? 0.6 : 1)
    }
}

/// The prototype's full-width buttons (`.btn`): 50 pt, radius 14. Kinds: filled accent, tinted,
/// gray, green (okb) and red (dangerb); `small` is 40 pt.
struct WideButtonStyle: ButtonStyle {
    enum Kind { case filled, tinted, gray, success, danger }
    var kind: Kind = .filled
    var small = false
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(small ? .subheadline.weight(.semibold) : .body.weight(.semibold))
            .frame(maxWidth: .infinity)
            .frame(height: small ? 40 : 50)
            .foregroundStyle(foreground)
            .background(RoundedRectangle(cornerRadius: small ? 12 : 14).fill(background))
            .opacity(enabled ? (configuration.isPressed ? 0.75 : 1) : 0.3)
    }

    private var foreground: AnyShapeStyle {
        switch kind {
        case .filled: AnyShapeStyle(.white)
        case .tinted: AnyShapeStyle(.tint)
        case .gray: AnyShapeStyle(.primary)
        case .success: AnyShapeStyle(Color.success)
        case .danger: AnyShapeStyle(Color.danger)
        }
    }

    private var background: AnyShapeStyle {
        switch kind {
        case .filled: AnyShapeStyle(.tint)
        case .tinted: AnyShapeStyle(.tint.opacity(0.12))
        case .gray: AnyShapeStyle(.fillTrack)
        case .success: AnyShapeStyle(Color.success.wash(0.13))
        case .danger: AnyShapeStyle(Color.danger.wash(0.12))
        }
    }
}

extension ButtonStyle where Self == WideButtonStyle {
    static var wide: WideButtonStyle { WideButtonStyle() }
    static func wide(_ kind: WideButtonStyle.Kind, small: Bool = false) -> WideButtonStyle { WideButtonStyle(kind: kind, small: small) }
}

/// A translucent bar pinned to the bottom of a page for its main action ("添加待办", "记一笔买入").
/// Use with `.safeAreaInset(edge: .bottom) { BottomBar { Button(…).buttonStyle(.wide) } }`.
struct BottomBar<Content: View>: View {
    @ViewBuilder var content: Content
    var body: some View {
        HStack(spacing: 10) { content }
            .padding(.horizontal, 16).padding(.top, 10).padding(.bottom, 10)
            .background {
                Rectangle().fill(.bar).ignoresSafeArea(edges: .bottom)
                    .overlay(alignment: .top) { Rectangle().fill(.hairline).frame(height: 0.5) }
            }
    }
}

/// A white rounded card (`.card`): 14 pt radius, 16 pt padding. For content outside a List.
struct CardBackground: ViewModifier {
    var padding: CGFloat = 16
    func body(content: Content) -> some View {
        content.padding(padding).frame(maxWidth: .infinity, alignment: .leading)
            .background(.white, in: RoundedRectangle(cornerRadius: 14))
    }
}

extension View {
    func card(padding: CGFloat = 16) -> some View { modifier(CardBackground(padding: padding)) }
}

// MARK: - check marks

/// The round to-do check (`.ck`): an empty ring, or filled with the accent and a check when done.
/// It pops when ticked. 44 pt tap target.
struct CheckButton: View {
    let isOn: Bool
    var size: CGFloat = 24
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            CheckMark(isOn: isOn, size: size)
                .frame(width: 44, height: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(isOn ? "标为未完成" : "完成")
        .accessibilityAddTraits(isOn ? .isSelected : [])
    }
}

/// The check circle on its own, for rows that are themselves the button (link pickers).
struct CheckMark: View {
    let isOn: Bool
    var size: CGFloat = 24
    @State private var pop = false

    var body: some View {
        ZStack {
            Circle().strokeBorder(Color(red: 60 / 255, green: 60 / 255, blue: 67 / 255).opacity(0.32), lineWidth: size < 22 ? 1.5 : 1.6)
                .opacity(isOn ? 0 : 1)
            Circle().fill(.tint).opacity(isOn ? 1 : 0)
            Image(systemName: "checkmark")
                .font(.system(size: size * 0.5, weight: .bold))
                .foregroundStyle(.white)
                .opacity(isOn ? 1 : 0)
        }
        .frame(width: size, height: size)
        .scaleEffect(pop ? 1.15 : 1)
        .animation(.snappy(duration: 0.2), value: isOn)
        .onChange(of: isOn) { _, on in
            guard on else { return }
            withAnimation(.spring(response: 0.18, dampingFraction: 0.5)) { pop = true }
            Task { try? await Task.sleep(for: .milliseconds(180)); withAnimation(.spring(response: 0.25, dampingFraction: 0.6)) { pop = false } }
        }
        .sensoryFeedback(.success, trigger: isOn) { _, new in new }
    }
}

/// The round 44 pt check-in button on goal rows (`.cbtn`): "+1", "+30", "+", or a check when the
/// period's target is met. Pops when tapped.
struct CheckInButton: View {
    let title: String?        // nil: a plus sign
    var done = false
    let action: () -> Void
    @State private var bump = 0

    var body: some View {
        Button {
            bump += 1
            action()
        } label: {
            ZStack {
                Circle().fill(done ? Color.success.wash(0.13) : Color.clear)
                if !done { Circle().fill(.tint.opacity(0.12)) }
                Group {
                    if done { Image(systemName: "checkmark").font(.system(size: 18, weight: .bold)) }
                    else if let title { Text(title).font(.footnote.weight(.bold)).monospacedDigit() }
                    else { Image(systemName: "plus").font(.system(size: 18, weight: .semibold)) }
                }
                .foregroundStyle(done ? AnyShapeStyle(Color.success) : AnyShapeStyle(.tint))
            }
            .frame(width: 44, height: 44)
            .contentShape(Circle())
        }
        .buttonStyle(PressScaleStyle(scale: 0.88))
        .phaseAnimator([1.0, 0.82, 1.12, 1.0], trigger: bump) { v, s in v.scaleEffect(s) } animation: { _ in .snappy(duration: 0.14) }
        .sensoryFeedback(.increase, trigger: bump)
        .accessibilityLabel("打卡")
    }
}

/// The flame and streak count ("🔥4周"), orange.
struct StreakLabel: View {
    let count: Int
    let unit: String
    var body: some View {
        HStack(spacing: 2) {
            Image(systemName: "flame.fill").font(.system(size: 11))
            Text("\(count)\(unit)").font(.footnote.weight(.semibold)).monospacedDigit()
        }
        .foregroundStyle(.warning)
        .accessibilityLabel("连续 \(count)\(unit)")
    }
}

/// A − / + pair (`.stepper`), 32 pt tall, gray.
struct CompactStepper: View {
    let onMinus: () -> Void
    let onPlus: () -> Void
    var body: some View {
        HStack(spacing: 0) {
            Button(action: onMinus) { Image(systemName: "minus").frame(width: 46, height: 32).contentShape(Rectangle()) }
                .accessibilityLabel("减少")
            Rectangle().fill(.hairline).frame(width: 0.5, height: 18)
            Button(action: onPlus) { Image(systemName: "plus").frame(width: 46, height: 32).contentShape(Rectangle()) }
                .accessibilityLabel("增加")
        }
        .font(.system(size: 16, weight: .medium))
        .foregroundStyle(.primary)
        .buttonStyle(.plain)
        .background(.fillTrack, in: RoundedRectangle(cornerRadius: 8))
    }
}

// MARK: - avatar

/// The round initials avatar ("LL") on 今天 and 设置.
struct Avatar: View {
    let name: String
    var size: CGFloat = 32
    var body: some View {
        Text(Self.initials(name))
            .font(.system(size: size * 0.4, weight: .semibold))
            .tracking(0.3)
            .foregroundStyle(Color(hex: 0x1F3FA8))
            .frame(width: size, height: size)
            .background(Circle().fill(LinearGradient(colors: [Color(hex: 0xDDE5FF), Color(hex: 0xC6D3FF)],
                                                     startPoint: .topLeading, endPoint: .bottomTrailing)))
            .accessibilityHidden(true)
    }

    /// "Liang Li" → "LL", "李亮" → "李", "" → "N".
    static func initials(_ name: String) -> String {
        let words = name.split(whereSeparator: { $0 == " " || $0 == "." || $0 == "_" || $0 == "-" })
        guard let first = words.first?.first else { return "N" }
        if !first.isASCII { return String(first) }
        let second = words.dropFirst().first?.first
        return (String(first) + (second.map(String.init) ?? "")).uppercased()
    }
}
