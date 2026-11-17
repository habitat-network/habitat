import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import {
  Trace,
  TraceCollapsible,
  TraceCollapsibleContent,
  TraceCollapsibleTrigger,
  TraceIcon,
  TraceLine,
  TraceTitle,
} from "./trace";

afterEach(cleanup);

describe("Trace", () => {
  it("marks a line's status and renders its title", () => {
    render(
      <Trace>
        <TraceLine status="error">
          <TraceIcon />
          <TraceTitle>Failed search</TraceTitle>
        </TraceLine>
      </Trace>,
    );
    const title = screen.getByText("Failed search");
    expect(
      title.closest("[data-slot=trace-line]")?.getAttribute("data-status"),
    ).toBe("error");
  });

  it("renders a collapsible trigger with its title", () => {
    render(
      <Trace>
        <TraceCollapsible>
          <TraceCollapsibleTrigger>
            <TraceLine>
              <TraceTitle>Ran search</TraceTitle>
            </TraceLine>
          </TraceCollapsibleTrigger>
          <TraceCollapsibleContent>details</TraceCollapsibleContent>
        </TraceCollapsible>
      </Trace>,
    );
    expect(screen.getByText("Ran search")).toBeTruthy();
  });
});
