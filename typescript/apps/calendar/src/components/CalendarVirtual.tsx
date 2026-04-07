import { useCallback, useEffect, useRef, useState } from "react";
import { defaultRangeExtractor, useVirtualizer } from "@tanstack/react-virtual";
import type { Range } from "@tanstack/react-virtual";
import FullCalendar from "@fullcalendar/react";
import dayGridPlugin from "@fullcalendar/daygrid";
import { EventSourceInput } from "@fullcalendar/core/index.js";
const ROW_HEIGHT = 360;
const MONTH_HEADER_HEIGHT = 0;
const MS_PER_WEEK = 7 * 24 * 60 * 60 * 1000;
const TOTAL_WEEKS = 500 * 53;

const WEEK_DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const MONTH_NAMES = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];

interface WeekRowProps {
  sunday: Date;
  events: EventSourceInput;
}

const WeekRow = ({ sunday, events }: WeekRowProps) => {
  return (
    <div className="h-full ml-8 border-l">
      <FullCalendar
        events={events}
        plugins={[dayGridPlugin]}
        initialView="dayGridWeek"
        headerToolbar={false}
        height="100%"
        initialDate={sunday}
        dayHeaderFormat={{ day: "numeric" }}
      />
    </div>
  );
};

const startDate = new Date(Date.UTC(1700, 0, 1));

function startOfWeek(date: Date) {
  const d = new Date(
    Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()),
  );
  const day = d.getUTCDay();
  d.setUTCDate(d.getUTCDate() - day);
  return d;
}

// The actual first Sunday in our range
const firstSunday = startOfWeek(startDate);
const baseYear = firstSunday.getUTCFullYear();
const baseMonth = firstSunday.getUTCMonth();

// Total months spanned by the range
const lastSunday = new Date(
  firstSunday.getTime() + (TOTAL_WEEKS - 1) * MS_PER_WEEK,
);
const TOTAL_MONTHS =
  (lastSunday.getUTCFullYear() - baseYear) * 12 +
  lastSunday.getUTCMonth() -
  baseMonth +
  1;
const TOTAL_ROWS = TOTAL_WEEKS + TOTAL_MONTHS;

// m-th month (0-indexed from base month)
function monthInfo(m: number): { year: number; month: number } {
  const total = baseYear * 12 + baseMonth + m;
  return { year: Math.floor(total / 12), month: total % 12 };
}

// First week index whose Sunday falls in the m-th month
function firstWeekOfMonth(m: number): number {
  const { year, month } = monthInfo(m);
  const first = new Date(Date.UTC(year, month, 1));
  const dow = first.getUTCDay();
  const sunday = new Date(Date.UTC(year, month, 1 + (dow === 0 ? 0 : 7 - dow)));
  return Math.max(
    0,
    Math.round((sunday.getTime() - firstSunday.getTime()) / MS_PER_WEEK),
  );
}

// Virtual index where month m's header row appears
function headerIndex(m: number): number {
  return firstWeekOfMonth(m) + m;
}

// Binary search: find the month that owns virtual index v
function findMonthForVirtual(v: number): number {
  let lo = 0;
  let hi = TOTAL_MONTHS - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (headerIndex(mid) <= v) lo = mid;
    else hi = mid - 1;
  }
  return lo;
}

function isHeader(v: number): boolean {
  return headerIndex(findMonthForVirtual(v)) === v;
}

function weekIndexFromVirtual(v: number): number {
  const m = findMonthForVirtual(v);
  return firstWeekOfMonth(m) + (v - headerIndex(m) - 1);
}

function getSunday(weekIndex: number): Date {
  return new Date(firstSunday.getTime() + weekIndex * MS_PER_WEEK);
}

function getMonthLabel(m: number): string {
  const { year, month } = monthInfo(m);
  return `${MONTH_NAMES[month]} ${year}`;
}

// Count how many month headers appear at or before the given week index
function countMonthsUpToWeek(weekIndex: number): number {
  let lo = 0;
  let hi = TOTAL_MONTHS - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (firstWeekOfMonth(mid) <= weekIndex) lo = mid;
    else hi = mid - 1;
  }
  if (firstWeekOfMonth(0) > weekIndex) return 0;
  return lo + 1;
}

function getInitialOffset(selectedDate: Date): number {
  const targetWeek = startOfWeek(selectedDate);
  const weekIdx = Math.round(
    (targetWeek.getTime() - firstSunday.getTime()) / MS_PER_WEEK,
  );
  const monthsBefore = countMonthsUpToWeek(weekIdx);
  return weekIdx * ROW_HEIGHT + monthsBefore * MONTH_HEADER_HEIGHT;
}

interface CalendarVirtualProps {
  date: Date;
  events: EventSourceInput;
}

const CalendarVirtual = ({ date, events }: CalendarVirtualProps) => {
  const scrollRef = useRef(null);
  const activeStickyIndexRef = useRef(0);

  const rowVirtualizer = useVirtualizer({
    count: TOTAL_ROWS,
    initialOffset: () => getInitialOffset(date),
    getScrollElement: () => scrollRef.current,
    estimateSize: (index) =>
      isHeader(index) ? MONTH_HEADER_HEIGHT : ROW_HEIGHT,
    rangeExtractor: useCallback((range: Range) => {
      const m = findMonthForVirtual(range.startIndex);
      activeStickyIndexRef.current = headerIndex(m);
      const next = new Set([
        activeStickyIndexRef.current,
        ...defaultRangeExtractor(range),
      ]);
      return [...next].sort((a, b) => a - b);
    }, []),
  });

  useEffect(() => {
    rowVirtualizer.scrollToOffset(getInitialOffset(date), {
      behavior: "smooth",
    });
  }, [date]);

  return (
    <div className="h-full w-full flex flex-col bg-background">
      <div className="flex ml-8 border-b text-accent-foreground border-l text-sm">
        {WEEK_DAYS.map((day) => (
          <span key={day} className="flex-1 text-center">
            {day}
          </span>
        ))}
      </div>
      <div ref={scrollRef} className="overflow-y-auto calendar-virtual">
        <div
          className="relative"
          style={{ height: rowVirtualizer.getTotalSize() }}
        >
          {rowVirtualizer.getVirtualItems().map((virtualItem) => {
            const header = isHeader(virtualItem.index);
            const isActive = activeStickyIndexRef.current === virtualItem.index;
            return (
              <div
                key={virtualItem.key}
                className="top-0 left-0 w-full"
                style={{
                  ...(isActive
                    ? { position: "sticky" }
                    : {
                      position: "absolute",
                      transform: `translateY(${virtualItem.start}px)`,
                    }),
                  height: `${virtualItem.size}px`,
                }}
              >
                {header ? (
                  <span className="absolute month bg-white p-2">
                    {getMonthLabel(findMonthForVirtual(virtualItem.index))}
                  </span>
                ) : (
                  <WeekRow
                    events={events}
                    sunday={getSunday(weekIndexFromVirtual(virtualItem.index))}
                  />
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
};

export default CalendarVirtual;
