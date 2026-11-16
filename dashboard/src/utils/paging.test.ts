import { describe, expect, it } from "vitest";
import { pageWindow } from "./paging";

describe("pageWindow", () => {
  it("splits into pages of the given size", () => {
    expect(pageWindow(51, 0, 15)).toEqual({ page: 0, pages: 4, start: 0, end: 15 });
    expect(pageWindow(51, 1, 15)).toEqual({ page: 1, pages: 4, start: 15, end: 30 });
    expect(pageWindow(51, 3, 15)).toEqual({ page: 3, pages: 4, start: 45, end: 51 }); // short last page
    expect(pageWindow(45, 2, 15)).toEqual({ page: 2, pages: 3, start: 30, end: 45 }); // exact multiple
  });
  it("clamps a page that no longer exists after the list shrank", () => {
    expect(pageWindow(20, 3, 15)).toEqual({ page: 1, pages: 2, start: 15, end: 20 });
    expect(pageWindow(20, -2, 15).page).toBe(0);
    expect(pageWindow(20, Number.NaN, 15).page).toBe(0);
  });
  it("treats an empty list as one empty page", () => {
    expect(pageWindow(0, 5, 15)).toEqual({ page: 0, pages: 1, start: 0, end: 0 });
  });
});
