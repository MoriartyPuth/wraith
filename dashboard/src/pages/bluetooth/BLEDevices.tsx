import { PageHeader } from "../../components/ui";
import { BTDeviceList } from "../../components/bluetooth/BTDeviceList";

export default function BLEDevices() {
  return (
    <>
      <PageHeader title="Bluetooth Low Energy"
        subtitle="Advertisers heard by passive BLE scanning. One row per address — random addresses rotate, so one physical device can appear as several rows." />
      <BTDeviceList transport="ble" />
      <p className="mt-3 text-xs text-ink-3">
        Passive scanning transmits nothing. Manufacturer and service names come from the advertising data; any device can
        advertise any value. Classification is the sensor's inference, not an identification.
      </p>
    </>
  );
}
