import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/api", () => ({
  api: { getManagerName: vi.fn(), putManagerName: vi.fn() },
}));

import { api } from "@/lib/api";
import { DEFAULT_MANAGER_NAME, refreshManagerName, resetManagerNameForTests, saveManagerName } from "@/lib/manager-name";

const getMock = vi.mocked(api.getManagerName);
const putMock = vi.mocked(api.putManagerName);
const setting = (manager_name: string) => ({ manager_name, default: "JJ", is_default: manager_name === "JJ" });

beforeEach(() => {
  resetManagerNameForTests();
  getMock.mockReset();
  putMock.mockReset();
  window.localStorage.clear();
});

describe("manager name", () => {
  it("fetches the saved name once per page load, however many components ask", async () => {
    getMock.mockResolvedValue(setting("Jarvis"));
    await Promise.all([refreshManagerName(), refreshManagerName(), refreshManagerName()]);
    await refreshManagerName();
    expect(getMock).toHaveBeenCalledTimes(1);
    expect(window.localStorage.getItem("griffin_manager_name")).toBe("Jarvis");
  });

  it("keeps showing what it has when the server cannot be reached, and tries again next time", async () => {
    getMock.mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce(setting("Jarvis"));
    await refreshManagerName();
    expect(window.localStorage.getItem("griffin_manager_name")).toBeNull();
    await refreshManagerName();
    expect(getMock).toHaveBeenCalledTimes(2);
    expect(window.localStorage.getItem("griffin_manager_name")).toBe("Jarvis");
  });

  it("saving shows the server's cleaned-up name everywhere", async () => {
    putMock.mockResolvedValue(setting("Mr Stark"));
    await expect(saveManagerName("  Mr   Stark ")).resolves.toBe("Mr Stark");
    expect(putMock).toHaveBeenCalledWith("  Mr   Stark ");
    expect(window.localStorage.getItem("griffin_manager_name")).toBe("Mr Stark");
  });

  it("a refused name throws the server's message and changes nothing", async () => {
    putMock.mockRejectedValue(new Error("the name may use letters, digits, spaces, hyphens and apostrophes only"));
    await expect(saveManagerName("<b>")).rejects.toThrow(/letters, digits/);
    expect(window.localStorage.getItem("griffin_manager_name")).toBeNull();
  });

  it("the default is JJ", () => {
    expect(DEFAULT_MANAGER_NAME).toBe("JJ");
  });
});
