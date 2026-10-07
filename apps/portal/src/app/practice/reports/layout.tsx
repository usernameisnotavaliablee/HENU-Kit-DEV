import type { Metadata } from "next";
import { pageTitle } from "@/lib/seo";

// 页面是客户端组件，不能导出 metadata；标题由这一段的布局给出。
export const metadata: Metadata = { title: pageTitle("学习报告", "practice") };

export default function ReportsLayout({ children }: { children: React.ReactNode }) {
  return children;
}
