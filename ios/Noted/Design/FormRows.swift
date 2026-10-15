import SwiftUI
import UIKit

// Rows for inset-grouped forms (新建, detail pages, 设置): a colored square icon, a label, and a value,
// switch, picker or chevron on the right. They are ordinary List rows; compose them with native
// controls so menus, toggles and pickers behave natively:
//
//   Section {
//       DateToggleRows(date: $due)                                  // 日期 switch + inline calendar
//       Picker(selection: $lead) { … } label: { FieldLabel("提醒", icon: "bell", tile: .orange) }
//       Toggle(isOn: $pinned) { FieldLabel("置顶", icon: "pin", tile: .orange) }
//       NavigationLink(value: Route.theme) { FieldLabel("主题", icon: "paintpalette", tile: .orange, value: "蓝色") }
//       FieldRow("下次", icon: "clock", tile: .orange, value: "11月15日 周日")
//   }

/// The 30 pt rounded square with a white SF Symbol (`.tile`).
struct IconTile: View {
    let systemName: String
    var tile: TileColor = .blue
    var size: CGFloat = 30
    var body: some View {
        Image(systemName: systemName)
            .font(.system(size: size * 0.53, weight: .medium))
            .foregroundStyle(.white)
            .frame(width: size, height: size)
            .background(tile.color, in: RoundedRectangle(cornerRadius: size * 0.233))
            .accessibilityHidden(true)
    }
}

/// Icon tile + title (+ a small accent line under it, + a gray value on the right).
/// Use as the label of Toggle / Picker / NavigationLink / Button rows.
struct FieldLabel: View {
    @Environment(Session.self) private var session
    let title: String
    let icon: String
    var tile: TileColor = .blue
    /// Small accent text under the title ("10月10日 周六" under 日期).
    var detail: String?
    /// Gray text on the right (for NavigationLink and Button rows; pickers show their own).
    var value: String?

    init(_ title: String, icon: String, tile: TileColor = .blue, detail: String? = nil, value: String? = nil) {
        self.title = title; self.icon = icon; self.tile = tile; self.detail = detail; self.value = value
    }

    var body: some View {
        HStack(spacing: 12) {
            IconTile(systemName: icon, tile: tile)
            VStack(alignment: .leading, spacing: 0) {
                Text(title).foregroundStyle(.primary)
                // The accent itself: inside a Toggle `.tint` is the switch's green.
                if let detail { Text(detail).font(.footnote).foregroundStyle(session.accent) }
            }
            .alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
            if let value {
                Spacer(minLength: 8)
                Text(value).foregroundStyle(.secondaryText).lineLimit(1).monospacedDigit()
            }
        }
    }
}

/// A row that only shows a value, or runs `action` (then with a chevron).
struct FieldRow: View {
    let title: String
    let icon: String
    var tile: TileColor = .blue
    var value: String?
    var action: (() -> Void)?

    init(_ title: String, icon: String, tile: TileColor = .blue, value: String? = nil, action: (() -> Void)? = nil) {
        self.title = title; self.icon = icon; self.tile = tile; self.value = value; self.action = action
    }

    var body: some View {
        if let action {
            Button(action: action) {
                HStack {
                    FieldLabel(title, icon: icon, tile: tile, value: value)
                    Image(systemName: "chevron.right").font(.system(size: 13, weight: .semibold)).foregroundStyle(.tertiaryText)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        } else {
            FieldLabel(title, icon: icon, tile: tile, value: value)
        }
    }
}

// MARK: - date and time

/// 日期: a switch row (on = has a date) whose label opens an inline calendar under it. With
/// `allowsNone` false the switch is hidden (an event always has a date).
struct DateToggleRows: View {
    @Binding var date: Date?
    var title = "日期"
    var icon = "calendar"
    var tile: TileColor = .red
    var allowsNone = true
    /// Opens the calendar from outside (the 选择… chip).
    @Binding var expanded: Bool

    init(date: Binding<Date?>, title: String = "日期", icon: String = "calendar", tile: TileColor = .red,
         allowsNone: Bool = true, expanded: Binding<Bool> = .constant(false)) {
        _date = date; self.title = title; self.icon = icon; self.tile = tile; self.allowsNone = allowsNone; _expanded = expanded
    }

    @State private var open = false

    var body: some View {
        let isOpen = (open || expanded) && date != nil
        Group {
            if allowsNone {
                Toggle(isOn: Binding(get: { date != nil }, set: { on in
                    withAnimation(.snappy) {
                        date = on ? (date ?? Date.now.startOfDay) : nil
                        open = on
                        if !on { expanded = false }
                    }
                })) { label }
                .tint(.switchOn)
            } else {
                label
            }
            if isOpen, let d = date {
                DatePicker("", selection: Binding(get: { d }, set: { new in
                    date = keepTime(of: d, on: new)
                }), displayedComponents: .date)
                .datePickerStyle(.graphical)
                .labelsHidden()
                .environment(\.locale, Fmt.zh)
                .environment(\.calendar, Fmt.calendar)
            }
        }
    }

    private var label: some View {
        Button {
            guard date != nil else { return }
            withAnimation(.snappy) {
                if open || expanded { open = false; expanded = false } else { open = true }
            }
        } label: {
            FieldLabel(title, icon: icon, tile: tile, detail: date.map { Fmt.mdw($0) },
                       value: allowsNone ? nil : date.map { Fmt.mdw($0) })
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func keepTime(of old: Date, on day: Date) -> Date {
        let m = old.minuteOfDay
        return day.startOfDay.at(hour: m / 60, minute: m % 60)
    }
}

/// 时间: a switch row (on = a clock time on `date`'s day) with an inline wheel under its label.
/// Off sets the time to 00:00 (a date-only task).
struct TimeToggleRows: View {
    @Binding var date: Date
    var title = "时间"
    @State private var open = false

    var body: some View {
        let hasTime = date.minuteOfDay != 0
        Toggle(isOn: Binding(get: { hasTime }, set: { on in
            withAnimation(.snappy) {
                if on {
                    let next = min((Date.now.minuteOfDay / 60 + 1), 23)
                    date = date.isSameDay(as: .now) ? date.at(hour: next) : date.at(hour: 9)
                } else {
                    date = date.startOfDay
                }
                open = on
            }
        })) {
            Button {
                guard hasTime else { return }
                withAnimation(.snappy) { open.toggle() }
            } label: {
                FieldLabel(title, icon: "clock", tile: .blue, detail: hasTime ? Fmt.hm(date) : nil).contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        }
        .tint(.switchOn)
        if open && hasTime {
            DatePicker("", selection: Binding(get: { date }, set: { new in
                // 00:00 means "no time"; nudge it so the switch stays on.
                date = new.minuteOfDay == 0 ? new.at(hour: 0, minute: 1) : new
            }), displayedComponents: .hourAndMinute)
            .datePickerStyle(.wheel)
            .labelsHidden()
            .frame(maxWidth: .infinity)
            .environment(\.locale, Locale(identifier: "en_GB"))
        }
    }
}

/// A row with a label and compact native date / time pickers on the right (日程's 开始 / 结束).
struct DateTimeRow: View {
    let title: String
    let icon: String
    var tile: TileColor = .red
    @Binding var date: Date
    var components: DatePickerComponents = [.date, .hourAndMinute]

    var body: some View {
        HStack {
            FieldLabel(title, icon: icon, tile: tile)
            Spacer(minLength: 8)
            DatePicker("", selection: $date, displayedComponents: components)
                .labelsHidden()
                .environment(\.locale, Fmt.zh)
        }
    }
}

// MARK: - pickers

/// 提醒 as a menu picker row.
struct ReminderPicker: View {
    @Binding var lead: ReminderLead
    var body: some View {
        Picker(selection: $lead) {
            ForEach(ReminderLead.allCases) { Text($0.label).tag($0) }
            if case .custom = lead { Text(lead.label).tag(lead) }
        } label: {
            FieldLabel("提醒", icon: "bell", tile: .orange)
        }
        .pickerStyle(.menu)
    }
}

/// 重复 as a menu picker row, with the weekday buttons under it for 每周.
/// `defaultWeekday` (1 = Sunday … 7) is selected when switching to 每周 with none chosen.
struct RepeatPicker: View {
    @Binding var rule: RepeatRule
    var defaultWeekday: Int = Fmt.calendar.component(.weekday, from: .now)

    var body: some View {
        Picker(selection: Binding(get: { rule.frequency }, set: { f in
            withAnimation(.snappy) {
                rule.frequency = f
                if f == .weekly && rule.weekdays.isEmpty { rule.weekdays = [defaultWeekday] }
            }
        })) {
            ForEach(RepeatRule.Frequency.allCases, id: \.self) { Text($0.label).tag($0) }
        } label: {
            FieldLabel("重复", icon: "repeat", tile: .gray)
        }
        .pickerStyle(.menu)
        if rule.frequency == .weekly {
            WeekdayPicker(days: $rule.weekdays)
        }
    }
}

/// 一 二 三 四 五 六 日 round toggles; at least one stays on.
struct WeekdayPicker: View {
    @Binding var days: Set<Int>
    var body: some View {
        HStack {
            ForEach(RepeatRule.weekOrder, id: \.self) { d in
                let on = days.contains(d)
                Button {
                    if on { if days.count > 1 { days.remove(d) } } else { days.insert(d) }
                } label: {
                    Text(Fmt.weekdayChar(d))
                        .font(.subheadline.weight(.medium))
                        .frame(width: 38, height: 38)
                        .foregroundStyle(on ? AnyShapeStyle(.white) : AnyShapeStyle(.primary))
                        .background(Circle().fill(on ? AnyShapeStyle(.tint) : AnyShapeStyle(.fillTrack)))
                }
                .buttonStyle(.plain)
                .accessibilityLabel("周\(Fmt.weekdayChar(d))")
                .accessibilityAddTraits(on ? .isSelected : [])
                if d != 1 { Spacer(minLength: 0) }
            }
        }
        .padding(.vertical, 4)
    }
}

/// 优先级 as a menu picker row.
struct PriorityPicker: View {
    @Binding var priority: Noted_V1_Priority
    var body: some View {
        Picker(selection: $priority) {
            ForEach(Noted_V1_Priority.choices, id: \.self) { Text($0.label).tag($0) }
        } label: {
            FieldLabel("优先级", icon: "flag", tile: .rust)
        }
        .pickerStyle(.menu)
    }
}

/// 空间: 个人 / 工作 segmented, on the right of the row.
struct SpaceSegmentRow: View {
    @Binding var space: Noted_V1_Space
    var body: some View {
        HStack {
            FieldLabel("空间", icon: "person", tile: .graphite)
            Spacer(minLength: 8)
            SpaceSegment(space: $space).frame(width: 132)
        }
    }
}

/// 个人 / 工作 as a segmented control.
struct SpaceSegment: View {
    @Binding var space: Noted_V1_Space
    var body: some View {
        Picker("空间", selection: Binding(get: { space == .work ? Noted_V1_Space.work : .life }, set: { space = $0 })) {
            Text("个人").tag(Noted_V1_Space.life)
            Text("工作").tag(Noted_V1_Space.work)
        }
        .pickerStyle(.segmented)
        .labelsHidden()
    }
}

/// 项目 as a menu of unarchived projects (with their space dots). Choosing a project also sets
/// `space` to the project's, when a binding is given.
struct ProjectPicker: View {
    @Environment(AppData.self) private var data
    @Binding var projectID: String
    var space: Binding<Noted_V1_Space>?

    var body: some View {
        Picker(selection: Binding(get: { projectID }, set: { id in
            projectID = id
            if let p = data.project(id) { space?.wrappedValue = p.space }
        })) {
            Text("无").tag("")
            Divider()
            ForEach(data.projects.filter { !$0.archived || $0.id == projectID }, id: \.id) { p in
                Label { Text(p.title) } icon: { Image(uiImage: .dot(p.space.tint)) }.tag(p.id)
            }
        } label: {
            FieldLabel("项目", icon: "folder", tile: .teal)
        }
        .pickerStyle(.menu)
    }
}

/// A multi-line text field for 备注, at least ~3 lines tall.
struct NotesField: View {
    @Binding var text: String
    var placeholder = "备注"
    var body: some View {
        TextField(placeholder, text: $text, axis: .vertical)
            .lineLimit(3...12)
            .frame(minHeight: 66, alignment: .top)
    }
}

extension UIImage {
    /// A colored dot that keeps its color inside menus (SF Symbols there are drawn monochrome).
    static func dot(_ color: Color, size: CGFloat = 10) -> UIImage {
        let r = UIGraphicsImageRenderer(size: CGSize(width: size, height: size))
        return r.image { ctx in
            UIColor(color).setFill()
            ctx.cgContext.fillEllipse(in: CGRect(x: 0, y: 0, width: size, height: size))
        }.withRenderingMode(.alwaysOriginal)
    }
}
