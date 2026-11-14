import { NodeDetail } from "../../operations";

export default async function Page({ params }: PageProps<"/admin/nodes/[code]">) {
  return <NodeDetail code={(await params).code} />;
}
