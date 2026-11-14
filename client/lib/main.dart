import 'dart:async';

import 'package:material_ui/material_ui.dart';

import 'screens/home_screen.dart';
import 'services/api_client.dart';
import 'services/tunnel_controller.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const TwentySixXApp());
}

class TwentySixXApp extends StatefulWidget {
  const TwentySixXApp({super.key});

  @override
  State<TwentySixXApp> createState() => _TwentySixXAppState();
}

class _TwentySixXAppState extends State<TwentySixXApp> {
  late final ApiClient _api = ApiClient();
  late final TunnelController _tunnel = TunnelController();

  @override
  void dispose() {
    _api.close();
    unawaited(_tunnel.dispose());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    const ink = Color(0xFF101C27);
    return MaterialApp(
      title: '26x VPN',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        useMaterial3: true,
        scaffoldBackgroundColor: const Color(0xFFF5F7F8),
        colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xFF087E79)),
        textTheme: ThemeData.light().textTheme.apply(
          bodyColor: ink,
          displayColor: ink,
        ),
      ),
      home: HomeScreen(api: _api, tunnel: _tunnel),
    );
  }
}
