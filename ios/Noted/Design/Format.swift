import Foundation

/// Chinese date, time, duration and number text, as the prototype writes them.
/// Every function that depends on "now" takes it as a parameter (default `.now`) so it can be tested.
enum Fmt {
    static let zh = Locale(identifier: "zh_CN")

    /// Gregorian, weeks start on Monday, the device's time zone.
    static var calendar: Calendar {
        var c = Calendar(identifier: .gregorian)
        c.locale = zh
        c.firstWeekday = 2
        c.timeZone = .current
        return c
    }

    private static let weekdayChars = Array("日一二三四五六")

    private static let hmFormatter: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "HH:mm"
        return f
    }()
    private static let ymdFormatter: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyy-MM-dd"
        return f
    }()

    // MARK: dates

    /// "2026-10-08"
    static func ymd(_ d: Date) -> String { ymdFormatter.string(from: d) }
    /// "09:00"
    static func hm(_ d: Date) -> String { hmFormatter.string(from: d) }
    /// "10月18日"
    static func md(_ d: Date) -> String {
        let c = calendar.dateComponents([.month, .day], from: d)
        return "\(c.month ?? 0)月\(c.day ?? 0)日"
    }
    /// "周六"
    static func weekday(_ d: Date) -> String { "周" + String(weekdayChars[calendar.component(.weekday, from: d) - 1]) }
    /// "六": the single character, for week strips and month grids.
    static func weekdayShort(_ d: Date) -> String { String(weekdayChars[calendar.component(.weekday, from: d) - 1]) }
    /// Calendar weekday number (1 = Sunday … 7 = Saturday) to "一".."日".
    static func weekdayChar(_ weekday: Int) -> String { String(weekdayChars[(weekday - 1 + 7) % 7]) }
    /// "10月10日 周六"
    static func mdw(_ d: Date) -> String { "\(md(d)) \(weekday(d))" }
    /// "2026年10月"
    static func ym(_ d: Date) -> String {
        let c = calendar.dateComponents([.year, .month], from: d)
        return "\(c.year ?? 0)年\(c.month ?? 0)月"
    }
    /// "10月"
    static func month(_ d: Date) -> String { "\(calendar.component(.month, from: d))月" }

    /// Whole days from `now`'s day to `d`'s day (negative in the past).
    static func dayDiff(_ d: Date, from now: Date = .now) -> Int {
        let c = calendar
        return c.dateComponents([.day], from: c.startOfDay(for: now), to: c.startOfDay(for: d)).day ?? 0
    }

    /// 今天 / 明天 / 昨天 / 后天 / 周X (later this week) / 10月18日.
    static func day(_ d: Date, now: Date = .now) -> String {
        let n = dayDiff(d, from: now)
        switch n {
        case 0: return "今天"
        case 1: return "明天"
        case -1: return "昨天"
        case 2: return "后天"
        default:
            if n > 2, let week = calendar.dateInterval(of: .weekOfYear, for: now), d < week.end { return weekday(d) }
            return md(d)
        }
    }

    /// "明天 09:30", or just the day when there is no time.
    static func dayTime(_ d: Date, hasTime: Bool = true, now: Date = .now) -> String {
        hasTime ? "\(day(d, now: now)) \(hm(d))" : day(d, now: now)
    }

    /// When something was last edited: today "10:42", yesterday "昨天", this past week "周三", else "10月3日".
    static func stamp(_ d: Date, now: Date = .now) -> String {
        let n = -dayDiff(d, from: now)
        if n <= 0 { return hm(d) }
        if n == 1 { return "昨天" }
        if n < 7 { return weekday(d) }
        return md(d)
    }

    /// "还剩 41 天" / "今天截止" / "已过期 3 天".
    static func daysLeft(_ n: Int) -> String {
        if n > 0 { return "还剩 \(n) 天" }
        if n == 0 { return "今天截止" }
        return "已过期 \(-n) 天"
    }

    // MARK: durations

    /// "45 分钟", "1 小时", "1 小时 30 分".
    static func duration(minutes m: Int) -> String {
        let h = m / 60, r = m % 60
        if h == 0 { return "\(r) 分钟" }
        return r == 0 ? "\(h) 小时" : "\(h) 小时 \(r) 分"
    }

    /// The length of a span, or nil when it is not positive.
    static func duration(from a: Date, to b: Date) -> String? {
        let m = Int(b.timeIntervalSince(a) / 60)
        return m > 0 ? duration(minutes: m) : nil
    }

    // MARK: numbers

    /// 3 → "3", 2.5 → "2.5".
    static func number(_ v: Double) -> String { v == v.rounded() ? String(Int(v)) : String(format: "%.1f", v) }

    /// Shares with up to four decimals and no trailing zeros.
    static func shares(_ v: Double) -> String {
        let s = String(format: "%.4f", v)
        return s.replacingOccurrences(of: "0+$", with: "", options: .regularExpression)
            .replacingOccurrences(of: "\\.$", with: "", options: .regularExpression)
    }

    /// "$1,234.56"; `signed` adds "+" to gains. Losses use the minus sign "−".
    static func money(_ v: Double, currency: String = "USD", signed: Bool = false, cents: Bool = true) -> String {
        let f = NumberFormatter()
        f.locale = Locale(identifier: "en_US")
        f.numberStyle = .decimal
        f.minimumFractionDigits = cents ? 2 : 0
        f.maximumFractionDigits = cents ? 2 : 0
        let body = f.string(from: NSNumber(value: abs(v))) ?? String(abs(v))
        let symbol: String
        switch currency.uppercased() {
        case "USD", "": symbol = "$"
        case "CNY", "RMB": symbol = "¥"
        case "EUR": symbol = "€"
        case "HKD": symbol = "HK$"
        default: symbol = currency.uppercased() + " "
        }
        let sign = v < 0 ? "−" : (signed && v > 0 ? "+" : "")
        return sign + symbol + body
    }

    /// A percentage given in percent units: 7.8 → "7.80%", signed "+7.80%", "−3.10%".
    static func percent(_ v: Double, signed: Bool = false, digits: Int = 2) -> String {
        let sign = v < 0 ? "−" : (signed && v > 0 ? "+" : "")
        return sign + String(format: "%.\(digits)f", abs(v)) + "%"
    }
}

extension Date {
    /// Midnight at the start of this date's day.
    var startOfDay: Date { Fmt.calendar.startOfDay(for: self) }
    /// The same time `n` days later.
    func adding(days n: Int) -> Date { Fmt.calendar.date(byAdding: .day, value: n, to: self) ?? self }
    /// Monday of this date's week, at midnight.
    var startOfWeek: Date { Fmt.calendar.dateInterval(of: .weekOfYear, for: self)?.start ?? startOfDay }
    /// The 1st of this date's month, at midnight.
    var startOfMonth: Date { Fmt.calendar.dateInterval(of: .month, for: self)?.start ?? startOfDay }
    func isSameDay(as other: Date) -> Bool { Fmt.calendar.isDate(self, inSameDayAs: other) }
    /// Minutes since midnight.
    var minuteOfDay: Int {
        let c = Fmt.calendar.dateComponents([.hour, .minute], from: self)
        return (c.hour ?? 0) * 60 + (c.minute ?? 0)
    }
    /// This day at hour:minute.
    func at(hour: Int, minute: Int = 0) -> Date {
        Fmt.calendar.date(bySettingHour: hour, minute: minute, second: 0, of: self) ?? self
    }
    /// Next Monday at midnight (the 下周一 chip).
    var nextMonday: Date { startOfWeek.adding(days: 7) }
}
